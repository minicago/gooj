package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/minicago/gooj/sql_service"
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
	// userDir := filepath.Join("data", "user", req.Username)
	// codePath := filepath.Join(userDir, req.Problem+".cpp")
	// create user in DB (if not exists) and create submission record
	if err := sql_service.CreateUserIfNotExists(req.Username); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	sub, err := sql_service.CreateSubmission(req.Username, req.Problem, req.Code)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "queued", "submission_id": sub.ID})
}

// func appendMessage(line string) {
// 	_ = os.MkdirAll("data", 0775)
// 	f, err := os.OpenFile("data/message.txt", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
// 	if err != nil {
// 		log.Printf("append message open failed: %v", err)
// 		return
// 	}
// 	defer f.Close()
// 	_, _ = f.WriteString(line + "\n")
// }

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
