package judge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	// "io/os"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/minicago/gooj/sql_service"
)

// StartJudge starts the judge loop as a goroutine. It polls the DB for queued submissions.
func StartJudge() {
	go func() {
		// ensure required docker images are present to avoid long pulls during processing
		// ensureDockerImage("gcc-with-time")
		for {
			sub, err := sql_service.PopQueuedSubmission()
			if err != nil {
				// no job or DB error; sleep briefly
				time.Sleep(time.Second)
				continue
			}
			processJob(sub)
		}
	}()
}

// ensureDockerImage pulls the given image (with timeout) so compile/run won't block on pulls
// func ensureDockerImage(image string) {
// 	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
// 	defer cancel()
// 	cmd := exec.CommandContext(ctx, "docker", "pull", image)
// 	var out bytes.Buffer
// 	cmd.Stdout = &out
// 	cmd.Stderr = &out
// 	if err := cmd.Run(); err != nil {
// 		log.Printf("docker pull %s failed: %v output=%s", image, err, out.String())
// 	} else {
// 		log.Printf("docker image %s available", image)
// 	}
// }

func appendMessage(line string) {
	_ = os.MkdirAll("data", 0755)
	f, err := os.OpenFile("data/message.txt", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("append message failed: %v", err)
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line + "\n")
}

func processJob(sub sql_service.Submission) {
	// create temp working dir under repository root ./tmp (ensure base exists)
	tmpBase := "./tmp"
	if err := os.MkdirAll(tmpBase, 0755); err != nil {
		log.Printf("failed to create tmp base dir %s: %v", tmpBase, err)
	}
	// ensure base has world-readable/executable so tools like `go build` won't fail when tmp subdirs exist
	_ = os.Chmod(tmpBase, 0755)
	tmpDir, err := os.MkdirTemp(tmpBase, fmt.Sprintf("sub-%d-", sub.ID))
	if err != nil {
		// fallback to system temp
		log.Printf("failed to create tmp in %s: %v, falling back to system temp", tmpBase, err)
		tmpDir, err = os.MkdirTemp("", fmt.Sprintf("sub-%d-", sub.ID))
		if err != nil {
			log.Printf("failed to create system tmp dir: %v", err)
			_ = sql_service.UpdateSubmissionResult(sub.ID, "internal_error", nil)
			appendMessage(fmt.Sprintf("%s submitted %s => INTERNAL_ERROR (tmp)", sub.Username, sub.Problem))
			return
		}
	}
	// try to make tmpDir world-readable/executable so other processes can inspect
	_ = os.Chmod(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	// write code file
	codePath := filepath.Join(tmpDir, "solution.cpp")
	if err := os.WriteFile(codePath, []byte(sub.Code), 0644); err != nil {
		log.Printf("failed to write code file: %v", err)
		_ = sql_service.UpdateSubmissionResult(sub.ID, "internal_error", nil)
		appendMessage(fmt.Sprintf("%s submitted %s => INTERNAL_ERROR (write)", sub.Username, sub.Problem))
		return
	}

	// verify file actually exists and is writable (some environments may hide errors)
	// if fi, err := os.Stat(codePath); err != nil {
	// 	log.Printf("code file stat failed after write: %v", err)
	// 	// fallback: try explicit open/create and write
	// 	f, ferr := os.OpenFile(codePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	// 	if ferr != nil {
	// 		log.Printf("fallback open failed for %s: %v", codePath, ferr)
	// 		_ = sql_service.UpdateSubmissionResult(sub.ID, "internal_error", nil)
	// 		appendMessage(fmt.Sprintf("%s submitted %s => INTERNAL_ERROR (write-fallback)", sub.Username, sub.Problem))
	// 		return
	// 	}
	// 	if _, werr := f.Write([]byte(sub.Code)); werr != nil {
	// 		log.Printf("fallback write failed for %s: %v", codePath, werr)
	// 		f.Close()
	// 		_ = sql_service.UpdateSubmissionResult(sub.ID, "internal_error", nil)
	// 		appendMessage(fmt.Sprintf("%s submitted %s => INTERNAL_ERROR (write-fallback2)", sub.Username, sub.Problem))
	// 		return
	// 	}
	// 	f.Close()
	// 	if fi2, err2 := os.Stat(codePath); err2 == nil {
	// 		log.Printf("code file created by fallback: %s size=%d mode=%v", codePath, fi2.Size(), fi2.Mode())
	// 	} else {
	// 		log.Printf("code file still missing after fallback: %v", err2)
	// 	}
	// } else {
	// 	log.Printf("code file created: %s size=%d mode=%v", codePath, fi.Size(), fi.Mode())
	// }

	// read problem config from disk
	cfgPath := filepath.Join("data", "problem", sub.Problem, "config.json")
	cfgData, _ := os.ReadFile(cfgPath)
	tests := 1
	timeLimit := 1.0
	memMB := 128
	if len(cfgData) > 0 {
		var obj map[string]any
		if err := json.Unmarshal(cfgData, &obj); err == nil {
			// tests: accept tests, tests_count, TestsCount
			if v, ok := obj["tests"].(float64); ok {
				tests = int(v)
			} else if v, ok := obj["tests_count"].(float64); ok {
				tests = int(v)
			}
			// time limit: accept time_limit (seconds) or time_limit_ms (milliseconds)
			if v, ok := obj["time_limit_ms"].(float64); ok {
				timeLimit = v / 1000.0
			} else if v, ok := obj["time_limit"].(float64); ok {
				timeLimit = v
			} else if v, ok := obj["time_limit_s"].(float64); ok {
				timeLimit = v
			}
			// memory: accept mem_mb or mem_limit_mb
			if v, ok := obj["mem_mb"].(float64); ok {
				memMB = int(v)
			} else if v, ok := obj["mem_limit_mb"].(float64); ok {
				memMB = int(v)
			}
		}
	}

	results := []sql_service.TestResult{}
	status := "ok"

	// compile inside docker
	// use absolute paths to avoid stray files
	absTmp, _ := filepath.Abs(tmpDir)
	// use absolute g++ path to avoid PATH issues inside image
	compileCmd := "g++ solution.cpp -O2 -std=c++17 -o solution 2>compile.err; if [ -s compile.err ]; then cat compile.err >&2; exit 2; fi"
	// compilation can require significantly more memory than runtime limits; raise compile memory cap
	compileMem := 512
	dockerCompileArgs := []string{"run", "--rm", "-v", absTmp + ":/work", "-w", "/work", "--network", "none", "--memory", fmt.Sprintf("%dm", compileMem), "--cpus", "1.0", "gcc-with-time", "bash", "-lc", fmt.Sprintf("%v", compileCmd)}
	// dockerCompileArgs := []string{"run", "--rm", "-v", absTmp + ":/work", "-w", "/work", "--network", "none", "--memory", fmt.Sprintf("%dm", compileMem), "--cpus", "1.0", "gcc:12", "bash", "-lc", compileCmd}
	// increase compile timeout to allow for image/pulled layers and heavier builds
	cctx, ccancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer ccancel()
	ccmd := exec.CommandContext(cctx, "docker", dockerCompileArgs...)
	var cerr bytes.Buffer
	var cout bytes.Buffer
	ccmd.Stderr = &cerr
	ccmd.Stdout = &cout
	// log.Printf("running compile: docker %s", strings.Join(dockerCompileArgs, " "))
	if err := ccmd.Run(); err != nil {
		// compile error
		status = "compile_error"
		outStr := cout.String() + "\n" + cerr.String()
		results = append(results, sql_service.TestResult{TestIndex: 0, Passed: false, Output: outStr, Expected: "", TimeMs: 0, MemoryKB: 0})
		_ = sql_service.UpdateSubmissionResult(sub.ID, status, results)
		appendMessage(fmt.Sprintf("%s submitted %s => COMPILE_ERROR : %v output=%s", sub.Username, sub.Problem, err, outStr))
		return
	}

	// helper to parse GNU time -v output
	parseTimeLog := func(path string) (timeMs int, memKB int) {
		data, err := os.ReadFile(path)
		if err != nil {
			return 0, 0
		}
		text := string(data)
		// parse Maximum resident set size (kbytes): 12345
		memRe := regexp.MustCompile(`Maximum resident set size \(kbytes\):\s*(\d+)`)
		if m := memRe.FindStringSubmatch(text); len(m) >= 2 {
			if v, err := strconv.Atoi(m[1]); err == nil {
				memKB = v
			}
		}
		// parse User time (seconds): 0.12 and System time (seconds): 0.01
		userRe := regexp.MustCompile(`User time \(seconds\):\s*([0-9.]+)`)
		sysRe := regexp.MustCompile(`System time \(seconds\):\s*([0-9.]+)`)
		var userF, sysF float64
		if m := userRe.FindStringSubmatch(text); len(m) >= 2 {
			if f, err := strconv.ParseFloat(m[1], 64); err == nil {
				userF = f
			}
		}
		if m := sysRe.FindStringSubmatch(text); len(m) >= 2 {
			if f, err := strconv.ParseFloat(m[1], 64); err == nil {
				sysF = f
			}
		}
		timeMs = int((userF + sysF) * 1000.0)
		return timeMs, memKB
	}

	// run tests sequentially
	for i := 1; i <= tests; i++ {
		inPath := filepath.Join("data", "problem", sub.Problem, fmt.Sprintf("%d.in", i))
		expectedPath := filepath.Join("data", "problem", sub.Problem, fmt.Sprintf("%d.out", i))
		// fmt.Printf("%v : %v -> %v\n", i, inPath, filepath.Join(absTmp, "in.in"))
		if b, err := os.ReadFile(inPath); err == nil {
			_ = os.WriteFile(filepath.Join(absTmp, "in.in"), b, 0644)
		} else {
			_ = os.WriteFile(filepath.Join(absTmp, "in.in"), []byte(""), 0644)
		}

		// run with GNU time writing verbose stats to time.log and save exit code to rc
		// ensure 'time' is available inside container by installing it if necessary (quietly)
		shellCmd := fmt.Sprintf("/usr/bin/time -v -o time.log timeout %ds ./solution < in.in > out.out 2>runtime.err; echo $? > rc; cat out.out", int(timeLimit+1))
		// shellCmd := fmt.Sprintf("timeout %ds /work/solution < in.in > out.out 2>runtime.err; echo $? > rc; cat out.out", int(timeLimit))
		dockerArgs := []string{"run", "--rm", "-v", absTmp + ":/work", "-w", "/work", "--network", "none", "--memory", fmt.Sprintf("%dm", memMB), "--pids-limit", "64", "--cpu-shares", "128", "gcc-with-time", "bash", "-lc", shellCmd}

		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeLimit+5)*time.Second)
		cmd := exec.CommandContext(ctx, "docker", dockerArgs...)
		var outb bytes.Buffer
		var errb bytes.Buffer
		cmd.Stdout = &outb
		cmd.Stderr = &errb
		err := cmd.Run()
		cancel()
		// for {
		// }

		if err != nil {
			// read rc if present to determine child exit
			rc := -1
			if b, e := os.ReadFile(filepath.Join(absTmp, "rc")); e == nil {
				if v, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
					rc = v
				}
			}
			stderr := errb.String()
			// context deadline -> treat as TLE
			if strings.Contains(err.Error(), "context deadline exceeded") || rc == 124 {
				status = "tle"
				// try to parse time.log for time used
				tms, _ := parseTimeLog(filepath.Join(absTmp, "time.log"))
				results = append(results, sql_service.TestResult{TestIndex: i, Passed: false, Output: "TLE", Expected: "", TimeMs: tms, MemoryKB: 0})
				_ = sql_service.UpdateSubmissionResult(sub.ID, status, results)
				appendMessage(fmt.Sprintf("%s submitted %s => TLE", sub.Username, sub.Problem))
				return
			}
			// exit code 137 often indicates OOM
			if rc == 137 {
				status = "mle"
				_, memKB := parseTimeLog(filepath.Join(absTmp, "time.log"))
				results = append(results, sql_service.TestResult{TestIndex: i, Passed: false, Output: "MLE", Expected: "", TimeMs: 0, MemoryKB: memKB})
				_ = sql_service.UpdateSubmissionResult(sub.ID, status, results)
				appendMessage(fmt.Sprintf("%s submitted %s => MLE", sub.Username, sub.Problem))
				return
			}
			if rc == 3 {
				status = "runtime_error"
				results = append(results, sql_service.TestResult{TestIndex: i, Passed: false, Output: stderr, Expected: "", TimeMs: 0, MemoryKB: 0})
				_ = sql_service.UpdateSubmissionResult(sub.ID, status, results)
				appendMessage(fmt.Sprintf("%s submitted %s => RUNTIME_ERROR", sub.Username, sub.Problem))
				return
			}
			status = "runtime_error"
			results = append(results, sql_service.TestResult{TestIndex: i, Passed: false, Output: stderr, Expected: "", TimeMs: 0, MemoryKB: 0})
			_ = sql_service.UpdateSubmissionResult(sub.ID, status, results)
			appendMessage(fmt.Sprintf("%s submitted %s => RUNTIME_ERROR", sub.Username, sub.Problem))
			return
		}

		expected, _ := os.ReadFile(expectedPath)
		got := outb.Bytes()
		normalize := func(b []byte) string {
			s := string(b)
			s = strings.ReplaceAll(s, "\r\n", "\n")
			s = strings.TrimSpace(s)
			return s
		}
		passed := normalize(got) == normalize(expected)
		// parse time.log for time and memory
		tms, memKB := parseTimeLog(filepath.Join(absTmp, "time.log"))
		results = append(results, sql_service.TestResult{TestIndex: i, Passed: passed, Output: string(got), Expected: string(expected), TimeMs: tms, MemoryKB: memKB})
		if !passed {
			status = "wa"
			_ = sql_service.UpdateSubmissionResult(sub.ID, status, results)
			appendMessage(fmt.Sprintf("%s submitted %s => WA", sub.Username, sub.Problem))
			return
		}
	}

	// all passed
	status = "ok"
	_ = sql_service.UpdateSubmissionResult(sub.ID, status, results)
	appendMessage(fmt.Sprintf("%s submitted %s => OK", sub.Username, sub.Problem))
}
