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

	// prepare input/output files inside userDir for docker
	inPath := filepath.Join("data/problem", req.Problem, "1.in")
	outPath := filepath.Join("data/problem", req.Problem, "1.out")
	// copy input to userDir/in.in
	inDest := filepath.Join(userDir, "in")
	if b, err := os.ReadFile(inPath); err == nil {
		_ = os.WriteFile(inDest, b, 0644)
	} else {
		// ensure empty input file exists
		_ = os.WriteFile(inDest, []byte(""), 0644)
	}

	expected := []byte{}
	if b, err := os.ReadFile(outPath); err == nil {
		expected = b
	}

	// run compilation and execution inside Docker (gcc image) to sandbox
	absUserDir, _ := filepath.Abs(userDir)
	// build a shell command: compile, if compile errors print to stderr and exit 2; else run with timeout and capture stdout
	shellCmd := fmt.Sprintf("g++ %s -O2 -std=c++14 -o solution 2>compile.err; if [ -s compile.err ]; then cat compile.err >&2; exit 2; fi; timeout 5s ./solution < in > out.out 2>runtime.err; if [ -s runtime.err ]; then cat runtime.err >&2; exit 3; fi; cat out.out", filepath.Base(codePath))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dockerArgs := []string{"run", "--rm", "-v", absUserDir + ":/work", "-w", "/work", "--network", "none", "--memory", "512m", "--cpus", "0.5", "gcc:12", "bash", "-lc", shellCmd}
	cmd := exec.CommandContext(ctx, "docker", dockerArgs...)
	var combinedOut bytes.Buffer
	cmd.Stdout = &combinedOut
	var combinedErr bytes.Buffer
	cmd.Stderr = &combinedErr
	if err := cmd.Run(); err != nil {
		// determine error type by exit code output. If combinedErr has content and exit code 2 => compile error; 3 => runtime error
		stderr := combinedErr.String()
		if strings.Contains(stderr, "error") && strings.Contains(err.Error(), "exit status 2") {
			resText := fmt.Sprintf("compile error: %s", stderr)
			_ = os.WriteFile(filepath.Join(userDir, req.Problem+".result"), []byte(resText), 0644)
			appendMessage(fmt.Sprintf("%s submitted %s => COMPILE_ERROR", req.Username, req.Problem))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "compile_error", "detail": stderr})
			return
		}
		// runtime or other docker error
		resText := fmt.Sprintf("runtime error: %v; stderr:%s", err, stderr)
		_ = os.WriteFile(filepath.Join(userDir, req.Problem+".result"), []byte(resText), 0644)
		appendMessage(fmt.Sprintf("%s submitted %s => RUNTIME_ERROR", req.Username, req.Problem))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "runtime_error", "detail": stderr})
		return
	}

	// success: combinedOut holds the program stdout
	got := combinedOut.Bytes()
	// normalize and compare
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
	resText := fmt.Sprintf("WA\n--- expected ---\n%s\n--- got ---\n%s", string(expected), string(got))
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
