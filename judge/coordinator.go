package judge

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/minicago/gooj/config"
	"github.com/minicago/gooj/sql_service"
)

// workerInfo tracks a connected judge worker.
type workerInfo struct {
	ID       string
	Capacity int
	LastSeen time.Time
	Assigned int
}

// coordinator distributes queued submissions to judge workers over HTTP and
// receives their results. It owns the in-memory assignment table; the database
// remains the source of truth for submission state.
type coordinator struct {
	mu          sync.Mutex
	workers     map[string]*workerInfo
	assignments map[uint]string    // submissionID -> workerID
	assignedAt  map[uint]time.Time // submissionID -> claim time
}

var coord = &coordinator{
	workers:     make(map[string]*workerInfo),
	assignments: make(map[uint]string),
	assignedAt:  make(map[uint]time.Time),
}

// StartCoordinator starts the HTTP coordinator and, unless disabled in config,
// also runs a local judge loop so the coordinator node itself contributes CPU.
func StartCoordinator() {
	// On (re)start, any submission left in "running" by a previous coordinator
	// process is reset to "queued" so it gets re-judged instead of stalling.
	requeueRunning()

	if config.GetCoordinatorLocalJudge() {
		go StartJudge()
	}

	go coord.reaper()

	addr := fmt.Sprintf(":%d", config.GetCoordinatorPort())
	mux := http.NewServeMux()
	// Protected endpoints require the shared auth token (if configured) to prevent
	// unauthorized task claiming / result injection on untrusted networks.
	mux.HandleFunc("/api/judge/task", coord.auth(coord.handleTask))
	mux.HandleFunc("/api/judge/result", coord.auth(coord.handleResult))
	mux.HandleFunc("/api/judge/heartbeat", coord.auth(coord.handleHeartbeat))
	mux.HandleFunc("/api/judge/workers", coord.auth(coord.handleWorkers))
	mux.HandleFunc("/api/judge/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	log.Printf("judge coordinator listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("coordinator server error: %v", err)
	}
}

// handleTask hands the next queued submission to a worker. It atomically claims
// the submission (status -> running) via PopQueuedSubmission and records the
// assignment so a dead worker's job can be re-queued later.
func (c *coordinator) handleTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	workerID := r.URL.Query().Get("worker")
	if workerID == "" {
		http.Error(w, "missing worker id", http.StatusBadRequest)
		return
	}

	sub, err := sql_service.PopQueuedSubmission()
	if err != nil {
		// No queued submission available right now.
		w.WriteHeader(http.StatusNoContent)
		return
	}

	c.mu.Lock()
	c.assignments[sub.ID] = workerID
	c.assignedAt[sub.ID] = time.Now()
	if wi, ok := c.workers[workerID]; ok {
		wi.Assigned++
	}
	c.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sub)
}

// handleResult receives a worker's judgment and persists it.
func (c *coordinator) handleResult(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		SubmissionID uint                     `json:"submission_id"`
		Status       string                   `json:"status"`
		Results      []sql_service.TestResult `json:"results"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	// If the submission was cancelled while being judged, discard the result.
	var cur sql_service.Submission
	if err := sql_service.DB().First(&cur, body.SubmissionID).Error; err == nil && cur.Status == "cancelled" {
		c.release(body.SubmissionID)
		writeJSON(w, map[string]string{"status": "discarded"})
		return
	}

	if err := sql_service.UpdateSubmissionResult(body.SubmissionID, body.Status, body.Results); err != nil {
		http.Error(w, "failed to store result", http.StatusInternalServerError)
		return
	}
	appendMessage(fmt.Sprintf("worker reported submission %d => %s", body.SubmissionID, body.Status))
	c.release(body.SubmissionID)
	writeJSON(w, map[string]string{"status": "ok"})
}

// handleHeartbeat lets workers report liveness and capacity.
func (c *coordinator) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		WorkerID string `json:"worker_id"`
		Capacity int    `json:"capacity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.WorkerID == "" {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	c.mu.Lock()
	if wi, ok := c.workers[body.WorkerID]; ok {
		wi.LastSeen = time.Now()
		if body.Capacity > 0 {
			wi.Capacity = body.Capacity
		}
	} else {
		c.workers[body.WorkerID] = &workerInfo{
			ID:       body.WorkerID,
			Capacity: body.Capacity,
			LastSeen: time.Now(),
		}
	}
	c.mu.Unlock()
	writeJSON(w, map[string]string{"status": "ok"})
}

// handleWorkers lists currently known workers (for monitoring).
func (c *coordinator) handleWorkers(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	list := make([]workerInfo, 0, len(c.workers))
	for _, wi := range c.workers {
		list = append(list, *wi)
	}
	c.mu.Unlock()
	writeJSON(w, map[string]interface{}{"workers": list})
}

// reaper periodically re-queues submissions assigned to dead/unresponsive workers
// or running longer than the maximum expected time, so a crashed worker cannot
// permanently stall a submission.
func (c *coordinator) reaper() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for subID, wid := range c.assignments {
			stale := false
			if wi, ok := c.workers[wid]; !ok || now.Sub(wi.LastSeen) > 60*time.Second {
				stale = true
			} else if now.Sub(c.assignedAt[subID]) > 5*time.Minute {
				stale = true
			}
			if stale {
				if err := sql_service.DB().Model(&sql_service.Submission{}).
					Where("id = ? AND status = ?", subID, "running").
					Update("status", "queued").Error; err == nil {
					log.Printf("reaper re-queued stalled submission %d", subID)
				}
				if wi, ok := c.workers[wid]; ok && wi.Assigned > 0 {
					wi.Assigned--
				}
				delete(c.assignments, subID)
				delete(c.assignedAt, subID)
			}
		}
		c.mu.Unlock()
	}
}

// release removes an assignment tracking entry.
func (c *coordinator) release(subID uint) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if wid, ok := c.assignments[subID]; ok {
		if wi, ok := c.workers[wid]; ok && wi.Assigned > 0 {
			wi.Assigned--
		}
		delete(c.assignments, subID)
		delete(c.assignedAt, subID)
	}
}

// requeueRunning resets any submission left "running" (e.g. from a previous
// coordinator process) back to "queued" so it is re-judged.
func requeueRunning() {
	if sql_service.DB() == nil {
		return
	}
	res := sql_service.DB().Model(&sql_service.Submission{}).
		Where("status = ?", "running").
		Update("status", "queued")
	if res.Error == nil && res.RowsAffected > 0 {
		log.Printf("coordinator re-queued %d stale running submission(s)", res.RowsAffected)
	}
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// auth wraps a handler, requiring the configured shared token when one is set.
// Workers pass the token via the ?token= query parameter or the X-Judge-Token header.
func (c *coordinator) auth(next http.HandlerFunc) http.HandlerFunc {
	token := config.GetJudgeAuthToken()
	if token == "" {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		provided := r.URL.Query().Get("token")
		if provided == "" {
			provided = r.Header.Get("X-Judge-Token")
		}
		if provided != token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}
