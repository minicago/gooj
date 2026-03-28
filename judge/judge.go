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

// JudgeConfig contains configuration for judging a single test case
type JudgeConfig struct {
	TimeLimit    float64 // time limit in seconds
	MemLimit     int     // memory limit in MB
	InputPath    string  // path to input file
	ExpectedPath string  // path to expected output file
	WorkTmpPath  string  // path to temporary working directory for this test
}

// JudgeResult contains the result of judging a single test case
type JudgeResult struct {
	RunTimeMs int    // execution time in milliseconds
	MemoryKB  int    // memory usage in kilobytes
	Passed    bool   // whether output matches (ignoring trailing spaces and newlines)
	Info      string // the differing character from output, empty if passed
	Status    string // "accepted", "time_limit_exceeded", "memory_limit_exceeded", "runtime_error", "wrong_answer"
}

// JudgeTest judges a single test case with the given configuration
// It runs the solution binary in a Docker container and returns the result
func JudgeTest(cfg JudgeConfig) JudgeResult {
	result := JudgeResult{
		RunTimeMs: 0,
		MemoryKB:  0,
		Passed:    false,
		Info:      "",
		Status:    "runtime_error",
	}

	// Read input file
	inputData, err := os.ReadFile(cfg.InputPath)
	if err != nil {
		result.Info = fmt.Sprintf("Failed to read input: %v", err)
		return result
	}

	// Write input file
	if err := os.WriteFile(filepath.Join(cfg.WorkTmpPath, "in.in"), inputData, 0644); err != nil {
		result.Info = fmt.Sprintf("Failed to write input: %v", err)
		return result
	}

	// Prepare Docker command with time and memory limits
	absTmp, _ := filepath.Abs(cfg.WorkTmpPath)

	shellCmd := fmt.Sprintf("/usr/bin/time -v -o time.log bash -c \"ulimit -t %d -m %d -s %d; ./solution < in.in > out.out 2>runtime.err; echo $? > rc; \" ", int(cfg.TimeLimit+1), cfg.MemLimit*1100, cfg.MemLimit*1100)
	// shellCmd := "/usr/bin/time -v -o time.log ./solution < in.in > out.out 2>runtime.err; echo $? > rc; cat out.out"
	dockerArgs := []string{
		"run", "--rm",
		"-v", absTmp + ":/work",
		"-w", "/work",
		"--network", "none",
		"--memory", fmt.Sprintf("%dm", cfg.MemLimit*2),
		"--pids-limit", "64",
		"--cpu-shares", "128",
		"gcc-with-time",
		"bash", "-lc", shellCmd,
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeLimit+5)*time.Second)
	cmd := exec.CommandContext(ctx, "docker", dockerArgs...)
	var outb bytes.Buffer
	var errb bytes.Buffer
	cmd.Stdout = &outb
	cmd.Stderr = &errb

	err = cmd.Run()
	cancel()

	// Parse time and memory from time.log
	parseTimeLog := func(path string) (timeMs int, memKB int) {
		data, err := os.ReadFile(path)
		if err != nil {
			return 0, 0
		}
		// fmt.Printf("time log:\n%s\n", string(data))
		text := string(data)
		memRe := regexp.MustCompile(`Maximum resident set size \(kbytes\):\s*(\d+)`)
		if m := memRe.FindStringSubmatch(text); len(m) >= 2 {
			if v, err := strconv.Atoi(m[1]); err == nil {
				memKB = v
			}
		}
		userRe := regexp.MustCompile(`User time \(seconds\):\s*([0-9.]+)`)
		var userF float64
		if m := userRe.FindStringSubmatch(text); len(m) >= 2 {
			if f, err := strconv.ParseFloat(m[1], 64); err == nil {
				userF = f
			}
		}
		timeMs = int((userF) * 1000.0)
		return timeMs, memKB
	}

	// Check for errors
	if err != nil {
		fmt.Printf("error : %v\n", err.Error())
		// Read return code
		rc := -1
		if b, e := os.ReadFile(filepath.Join(absTmp, "rc")); e == nil {
			if v, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				rc = v
				fmt.Printf("Return code: %d\n", rc)
			}
		}
		stderr := errb.String()

		if strings.Contains(err.Error(), "killed") || rc == 124 {
			result.Status = "time_limit_exceeded"
			result.Info = "Time limit exceeded"
			tms, _ := parseTimeLog(filepath.Join(absTmp, "time.log"))
			result.RunTimeMs = tms
			return result
		} else if rc == 137 {
			result.Status = "memory_limit_exceeded"
			result.Info = "Memory limit exceeded"
			_, memKB := parseTimeLog(filepath.Join(absTmp, "time.log"))
			result.MemoryKB = memKB
			return result
		} else {
			result.Status = "runtime_error"
			result.Info = stderr
			// Also capture any output that was produced
			if outb.Len() > 0 {
				result.Info += "\nProgram output:\n" + outb.String()
			}
			tms, memKB := parseTimeLog(filepath.Join(absTmp, "time.log"))
			result.RunTimeMs = tms
			result.MemoryKB = memKB
			return result
		}
	}

	result.RunTimeMs, result.MemoryKB = parseTimeLog(filepath.Join(absTmp, "time.log"))

	if result.RunTimeMs > int(cfg.TimeLimit*1000) {
		result.Status = "time_limit_exceeded"
		result.Info = "Time limit exceeded"
		return result
	}

	if result.MemoryKB > cfg.MemLimit*1024 {
		result.Status = "memory_limit_exceeded"
		result.Info = "Memory limit exceeded"
		return result
	}

	// Success - read output and compare with expected
	gotBytes, _ := os.ReadFile(filepath.Join(absTmp, "out.out"))
	expectedBytes, _ := os.ReadFile(cfg.ExpectedPath)

	// Normalize: convert \r\n to \n and trim trailing whitespace
	normalize := func(b []byte) string {
		s := string(b)
		s = strings.ReplaceAll(s, "\r\n", "\n")
		s = strings.TrimRight(s, " \t\n\r")
		return s
	}

	got := normalize(gotBytes)
	expected := normalize(expectedBytes)

	// Parse time and memory

	posGot := 0
	posExpected := 0

	lineNum := 1
	columnNum := 1

	for {
		if posGot >= len(got) && posExpected >= len(expected) {
			// Both ended, no mismatch found
			result.Passed = true
			result.Status = "accepted"
			result.Info = "Accepted"
			break
		}

		if posExpected >= len(expected) {
			if got[posGot] == '\n' {
				posGot++
				continue
			}
		}

		if posGot >= len(got) {
			if expected[posExpected] == '\n' {
				posExpected++
				continue
			}
		}

		if posGot >= len(got) || posExpected >= len(expected) {
			result.Passed = false
			result.Status = "wrong_answer"
			if posGot >= len(got) {
				result.Info = fmt.Sprintf("Output ended early in line %d, column %d, expected '%c'", lineNum, columnNum, expected[posExpected])
			} else {
				result.Info = fmt.Sprintf("Output has extra character '%c' in line %d, column %d", got[posGot], lineNum, columnNum)
			}
			break
		}

		if got[posGot] == '\n' && expected[posExpected] == ' ' {
			posExpected++
		} else if got[posGot] == ' ' && expected[posExpected] == '\n' {
			posGot++
		}

		if got[posGot] != expected[posExpected] {
			result.Passed = false
			result.Status = "wrong_answer"
			result.Info = fmt.Sprintf("Mismatch at line %d, column %d: got '%c', expected '%c'", lineNum, columnNum, got[posGot], expected[posExpected])
			break
		}

		if got[posGot] == '\n' {
			lineNum++
			columnNum = 1
		} else {
			columnNum++
		}

		posGot++
		posExpected++
	}

	return result
}

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
			appendMessage(fmt.Sprintf("%s submitted %d => INTERNAL_ERROR (tmp)", sub.Username, sub.ProblemID))
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
		appendMessage(fmt.Sprintf("%s submitted %d => INTERNAL_ERROR (write)", sub.Username, sub.ProblemID))
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
	cfgPath := filepath.Join("data", "problem", fmt.Sprintf("%d", sub.ProblemID), "config.json")
	cfgData, _ := os.ReadFile(cfgPath)

	timeLimit := 1.0
	memMB := 256

	if len(cfgData) > 0 {
		var obj map[string]any
		if err := json.Unmarshal(cfgData, &obj); err == nil {
			// tests: accept tests, tests_count, TestsCount
			// if v, ok := obj["tests"].(float64); ok {
			// 	tests = int(v)
			// } else if v, ok := obj["tests_count"].(float64); ok {
			// 	tests = int(v)
			// }
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
	cctx, ccancel := context.WithTimeout(context.Background(), 20*time.Second)
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
		results = append(results, sql_service.TestResult{TestIndex: 0, Passed: false, Output: outStr, TimeMs: 0, MemoryKB: 0})
		_ = sql_service.UpdateSubmissionResult(sub.ID, status, results)
		appendMessage(fmt.Sprintf("%s submitted %d => COMPILE_ERROR : %v output=%s", sub.Username, sub.ProblemID, err, outStr))
		return
	}

	// run tests sequentially using JudgeTest

	obj := make(map[string]any)
	_ = json.Unmarshal(cfgData, &obj)
	testGroups := []interface{}{}
	if v, ok := obj["test_cases"].([]interface{}); ok {
		testGroups = v
	} else {
		// return error if test groups not found; we require test groups to determine how many tests to run
		appendMessage(fmt.Sprintf("%s submitted %d => INTERNAL_ERROR (no test cases)", sub.Username, sub.ProblemID))
		_ = sql_service.UpdateSubmissionResult(sub.ID, "internal_error", nil)
		return
	}

	allPassed := true

	for _, testGroup := range testGroups {

		if testGroupMap, ok := testGroup.(map[string]any); !ok {
			// skip invalid test group
			_ = sql_service.UpdateSubmissionResult(sub.ID, "internal_error", nil)
			continue
		} else {
			// support both "tests" and "test_count" to specify number of tests in this group
			tests := []int{}
			if v, ok := testGroupMap["cases"].([]interface{}); ok {
				for _, caseVal := range v {
					if num, ok := caseVal.(float64); ok {
						tests = append(tests, int(num))
					}
				}
			} else {
				_ = sql_service.UpdateSubmissionResult(sub.ID, "internal_error", nil)
				continue
			}

			groupPassed := true

			for _, i := range tests {
				if !groupPassed {
					// skip remaining tests in this group if one already failed
					results = append(results, sql_service.TestResult{
						TestIndex: i,
						Passed:    false,
						Output:    "Skipped due to previous failure in group",
						TimeMs:    0,
						MemoryKB:  0,
						Status:    "skipped",
					})
					continue
				}

				inPath := filepath.Join("data", "problem", fmt.Sprintf("%d", sub.ProblemID), fmt.Sprintf("%d.in", i))
				expectedPath := filepath.Join("data", "problem", fmt.Sprintf("%d", sub.ProblemID), fmt.Sprintf("%d.ans", i))

				// Prepare configuration for this test
				cfg := JudgeConfig{
					TimeLimit:    timeLimit,
					MemLimit:     memMB,
					InputPath:    inPath,
					WorkTmpPath:  tmpDir,
					ExpectedPath: expectedPath,
				}

				// Run the test using the encapsulated function
				testResult := JudgeTest(cfg)

				// Convert JudgeResult to TestResult
				testIdx := i
				testPassed := testResult.Passed
				testOutput := testResult.Info // for WA, Info contains the mismatch details; for RE, it contains the error message
				testTimeMs := testResult.RunTimeMs
				testMemKB := testResult.MemoryKB
				testStatus := testResult.Status

				// Store test result
				results = append(results, sql_service.TestResult{
					TestIndex: testIdx,
					Passed:    testPassed,
					Output:    testOutput,
					// Expected:  testExpected,
					TimeMs:   testTimeMs,
					MemoryKB: testMemKB,
					Status:   testStatus,
					Score:    0, // scoring can be implemented later based on test groups or other criteria
				})

				// Handle different statuses
				if !testPassed {
					groupPassed = false
					allPassed = false
				}
			}

			if groupPassed {
				// if all tests in this group passed, we can continue to next group
				results[len(results)-1].Score = int(testGroupMap["score"].(float64)) // assign group score to last test in group; adjust as needed for different scoring schemes
			}
		}
	}

	// all passed
	if allPassed {
		status = "accepted"
	} else {
		status = "not accepted"
	}

	_ = sql_service.UpdateSubmissionResult(sub.ID, status, results)
	appendMessage(fmt.Sprintf("%s submitted %d => OK", sub.Username, sub.ProblemID))
}
