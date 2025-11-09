package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
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

	// prepare directories
	userDir := filepath.Join("data/user", req.Username)
	if err := os.MkdirAll(userDir, 0755); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		log.Printf("mkdir user dir failed: %v", err)
		return
	}

	// save code
	codePath := filepath.Join(userDir, req.Problem+".cpp")
	if err := os.WriteFile(codePath, []byte(req.Code), 0644); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		log.Printf("write code failed: %v", err)
		return
	}

	// compile
	binPath := filepath.Join(userDir, "solution")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "g++", codePath, "-O2", "-std=c++17", "-o", binPath)
	var cerr bytes.Buffer
	cmd.Stderr = &cerr
	if err := cmd.Run(); err != nil {
		// compilation failed — save result and respond
		resText := fmt.Sprintf("compile error: %s", cerr.String())
		_ = os.WriteFile(filepath.Join(userDir, req.Problem+".result"), []byte(resText), 0644)
		appendMessage(fmt.Sprintf("%s submitted %s => COMPILE_ERROR", req.Username, req.Problem))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "compile_error", "detail": cerr.String()})
		return
	}

	// run with input file data/<problem>/1.in
	inPath := filepath.Join("data/problem", req.Problem, "1.in")
	outPath := filepath.Join("data/problem", req.Problem, "1.out")
	var input []byte
	if b, err := os.ReadFile(inPath); err == nil {
		input = b
	}

	runCtx, runCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer runCancel()
	runCmd := exec.CommandContext(runCtx, binPath)
	runCmd.Stdin = bytes.NewReader(input)
	var runOut bytes.Buffer
	var runErr bytes.Buffer
	runCmd.Stdout = &runOut
	runCmd.Stderr = &runErr
	if err := runCmd.Run(); err != nil {
		resText := fmt.Sprintf("runtime error: %v; stderr:%s", err, runErr.String())
		_ = os.WriteFile(filepath.Join(userDir, req.Problem+".result"), []byte(resText), 0644)
		appendMessage(fmt.Sprintf("%s submitted %s => RUNTIME_ERROR", req.Username, req.Problem))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "runtime_error", "detail": runErr.String()})
		return
	}

	// read expected output
	expected := []byte{}
	if b, err := os.ReadFile(outPath); err == nil {
		expected = b
	}

	got := runOut.Bytes()
	// normalize newline endings and trim trailing whitespace
	normalize := func(b []byte) string {
		s := string(b)
		s = strings.ReplaceAll(s, "\r\n", "\n")
		s = strings.TrimSpace(s)
		return s
	}
	if normalize(got) == normalize(expected) {
		resText := "OK"
		_ = os.WriteFile(filepath.Join(userDir, req.Problem+".result"), []byte(resText), 0644)
		appendMessage(fmt.Sprintf("%s submitted %s => OK", req.Username, req.Problem))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	}

	// wrong answer
	resText := fmt.Sprintf("WA\n--- expected ---\n%s\n--- got ---\n%s", string(expected), runOut.String())
	_ = os.WriteFile(filepath.Join(userDir, req.Problem+".result"), []byte(resText), 0644)
	appendMessage(fmt.Sprintf("%s submitted %s => WA", req.Username, req.Problem))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "wa"})
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
