package judge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/minicago/gooj/config"
	"github.com/minicago/gooj/sql_service"
)

// StartWorker connects to a coordinator, pulls submissions, judges them
// concurrently and reports the results back. It does not touch the database
// directly; all state lives in the coordinator.
func StartWorker(coordinatorAddr, workerID string, concurrency int) {
	if coordinatorAddr == "" {
		coordinatorAddr = config.GetCoordinatorAddr()
	}
	if workerID == "" {
		workerID = generateWorkerID()
	}
	if concurrency < 1 {
		concurrency = config.GetWorkerConcurrency()
	}
	if concurrency < 1 {
		concurrency = 1
	}

	log.Printf("judge worker %s starting: coordinator=%s concurrency=%d", workerID, coordinatorAddr, concurrency)

	token := config.GetJudgeAuthToken()
	taskURL := fmt.Sprintf("%s/api/judge/task?worker=%s", coordinatorAddr, workerID)
	if token != "" {
		taskURL += "&token=" + token
	}

	// Heartbeat loop: announce liveness and capacity to the coordinator.
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		postHeartbeat(coordinatorAddr, workerID, concurrency, token)
		for range ticker.C {
			postHeartbeat(coordinatorAddr, workerID, concurrency, token)
		}
	}()

	// Worker pool: a bounded number of goroutines each pull and judge one task.
	sem := make(chan struct{}, concurrency)
	for {
		sub, ok := fetchTask(taskURL)
		if !ok {
			time.Sleep(time.Second)
			continue
		}
		sem <- struct{}{}
		go func(s sql_service.Submission) {
			defer func() { <-sem }()
			status, results := runJudge(s)
			if status == "" {
				status = "internal_error"
			}
			if err := postResult(coordinatorAddr, s.ID, status, results, token); err != nil {
				log.Printf("worker %s failed to report result for submission %d: %v", workerID, s.ID, err)
			}
		}(sub)
	}
}

// fetchTask asks the coordinator for the next queued submission.
func fetchTask(url string) (sql_service.Submission, bool) {
	resp, err := http.Get(url)
	if err != nil {
		return sql_service.Submission{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return sql_service.Submission{}, false
	}
	if resp.StatusCode != http.StatusOK {
		return sql_service.Submission{}, false
	}
	var sub sql_service.Submission
	if err := json.NewDecoder(resp.Body).Decode(&sub); err != nil {
		return sql_service.Submission{}, false
	}
	return sub, true
}

// postResult reports a judgment back to the coordinator.
func postResult(addr string, subID uint, status string, results []sql_service.TestResult, token string) error {
	body := map[string]interface{}{
		"submission_id": subID,
		"status":        status,
		"results":       results,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/api/judge/result", addr)
	if token != "" {
		url += "?token=" + token
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// postHeartbeat informs the coordinator this worker is alive and its capacity.
func postHeartbeat(addr, workerID string, capacity int, token string) {
	body, _ := json.Marshal(map[string]interface{}{
		"worker_id": workerID,
		"capacity":  capacity,
	})
	url := fmt.Sprintf("%s/api/judge/heartbeat", addr)
	if token != "" {
		url += "?token=" + token
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return
	}
	defer resp.Body.Close()
}

// generateWorkerID creates a stable-enough identifier for this worker process.
func generateWorkerID() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "worker"
	}
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}
