package web

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/minicago/gooj/file_service"
)

// SubmitRequest represents the JSON payload sent from the /code page
type SubmitRequest struct {
	Username string `json:"username"`
	Problem  string `json:"problem"`
	Code     string `json:"code"`
}

// SubmitHandler saves code, compiles and runs it against test input, saves result and appends to message.txt
func SubmitHandler(w http.ResponseWriter, r *http.Request) {
	var req SubmitRequest
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Problem = strings.TrimSpace(req.Problem)
	if req.Username == "" || req.Problem == "" || req.Code == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}

	// prepare directories and save code
	userDir := filepath.Join("data", "user", req.Username)
	codePath := filepath.Join(userDir, req.Problem+".cpp")
	svc := file_service.Default()
	if svc == nil {
		http.Error(w, "server not initialized", http.StatusInternalServerError)
		return
	}

	// ensure user dir and write code via file service
	if _, err := svc.ModifyFile(filepath.Join("data", "user", req.Username, ".touch"), func(_ []byte) ([]byte, error) {
		// noop modify just to ensure dir exists
		return []byte(""), nil
	}); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := svc.WriteFile(codePath, []byte(req.Code)); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// enqueue job into data/queue.json
	queuePath := filepath.Join("data", "queue.json")
	job := map[string]string{"username": req.Username, "problem": req.Problem, "code": codePath}
	_, err := svc.ModifyFile(queuePath, func(cur []byte) ([]byte, error) {
		var arr []map[string]string
		if len(cur) > 0 {
			if err := json.Unmarshal(cur, &arr); err != nil {
				// if corrupt, replace
				arr = []map[string]string{}
			}
		}
		arr = append(arr, job)
		return json.Marshal(arr)
	})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "queued"})
}

func appendMessage(line string) {
	_ = os.MkdirAll("data", 0775)
	f, err := os.OpenFile("data/message.txt", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("append message open failed: %v", err)
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line + "\n")
}

// ProblemsHandler returns the content of data/problem_list.json as-is
func ProblemsHandler(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile("data/problem_list.json")
	if err != nil {
		http.Error(w, "no problems", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

// ResultHandler returns the content of data/{user}/{problem}.result
func ResultHandler(w http.ResponseWriter, r *http.Request) {
	// expect path /result/{user}/{problem}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	user := parts[1]
	problem := parts[2]
	p := filepath.Join("data/user/", user, problem+".result")
	data, err := os.ReadFile(p)
	if err != nil {
		http.Error(w, "no result", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(data)
}
