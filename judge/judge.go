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
	"sync"
	"time"

	"github.com/minicago/gooj/config"
	"github.com/minicago/gooj/sql_service"
)

// JudgeConfig contains configuration for judging a single test case
type JudgeConfig struct {
	TimeLimit    float64 // time limit in seconds
	MemLimit     int     // memory limit in MB
	InputPath    string  // path to input file
	ExpectedPath string  // path to expected output file
	WorkTmpPath  string  // parent temporary dir (host path) where the compiled `solution` binary lives; mounted as /work
	TestIODir    string  // optional per-test subdir under WorkTmpPath for isolated I/O (in.in/out.out/...); enables concurrent multi-test judging
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

	// Prepare Docker command with time and memory limits. The parent tmp dir (which
	// holds the compiled `solution` binary) is mounted as /work so the binary is
	// always reachable; per-test I/O files are kept in an isolated subdir under it.
	absTmp, _ := filepath.Abs(cfg.WorkTmpPath)
	ioDir := absTmp
	if cfg.TestIODir != "" {
		ioDir = filepath.Join(absTmp, cfg.TestIODir)
		if err := os.MkdirAll(ioDir, 0755); err != nil {
			result.Info = fmt.Sprintf("Failed to create test work dir: %v", err)
			return result
		}
	}

	// Write input file (into the isolated I/O dir)
	if err := os.WriteFile(filepath.Join(ioDir, "in.in"), inputData, 0644); err != nil {
		result.Info = fmt.Sprintf("Failed to write input: %v", err)
		return result
	}

	// Simple shell command - run solution. Program output/errors are redirected to
	// files in the mounted work dir because a detached container's stdio is not
	// captured by the docker client. The compiled binary always lives at /work/
	// solution (the parent tmp dir, mounted as /work), so we run it via its
	// ABSOLUTE path to avoid "No such file or directory" when the cwd is not /work.
	// When TestIODir is set we cd into that subdir first (so concurrent tests never
	// clobber each other's in.in/out.out/time.log/rc); the binary is still reached
	// via the absolute /work/solution path.
	var shellCmd string
	if cfg.TestIODir != "" {
		shellCmd = fmt.Sprintf("cd %s && /usr/bin/time -v -o time.log /work/solution < in.in > out.out 2>runtime.err; echo $? > rc", cfg.TestIODir)
	} else {
		shellCmd = "/usr/bin/time -v -o time.log /work/solution < in.in > out.out 2>runtime.err; echo $? > rc"
	}
	dockerArgs := []string{
		"run", "--rm",
		"-v", absTmp + ":/work",
		"-w", "/work",
		"--network", "none",
		"--memory", fmt.Sprintf("%dm", cfg.MemLimit*2),
		"--pids-limit", "64",
		"--cpu-shares", "128",
		// Give the program a generous stack (tied to the memory limit, in bytes)
		// so legitimate deep recursion / large local arrays on big inputs do not
		// crash. The hard memory cap (--memory) still bounds total usage, so a
		// truly runaway recursion is stopped by the OOM killer (reported as MLE).
		// NOTE: the --ulimit stack value is in BYTES; a previous fixed value of
		// 262144 was only 256 KiB and caused correct solutions to stack-overflow
		// (SIGSEGV) on large tests and be mis-reported as "wrong answer".
		"--ulimit", fmt.Sprintf("stack=%d", cfg.MemLimit*1024*1024),
		"gcc-with-time",
		"bash", "-lc", shellCmd,
	}

	// Run the container detached and enforce the time limit ourselves. This is the
	// fix for the memory leak: previously exec.CommandContext killed the docker
	// CLIENT on timeout, but the container kept running inside the daemon and was
	// never removed (--rm only removes a container when *it* exits). Now the
	// container is always forcibly removed on timeout, so it cannot leak memory.
	timeout := time.Duration(int(cfg.TimeLimit*2)+5) * time.Second
	timedOut, runErr := runContainerDetached(dockerArgs, timeout)
	if runErr != nil {
		result.Info = fmt.Sprintf("Failed to run docker: %v", runErr)
		result.Status = "runtime_error"
		return result
	}

	// Parse time and memory from time.log
	parseTimeLog := func(path string) (timeMs int, memKB int) {
		data, err := os.ReadFile(path)
		if err != nil {
			return 0, 0
		}
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

	result.RunTimeMs, result.MemoryKB = parseTimeLog(filepath.Join(ioDir, "time.log"))

	// Read the program's exit code (written by the shell via "echo $? > rc").
	// IMPORTANT: this must be inspected on EVERY run, not only on timeout.
	// Previously a program that crashed (e.g. stack overflow or OOM on large
	// inputs) exited non-zero and produced no output, but because the exit code
	// was ignored the empty output was compared against the answer and wrongly
	// reported as "wrong answer" ("Output ended early"). We now map non-zero
	// exit codes to the proper runtime / memory / time error.
	rc := 0
	if b, e := os.ReadFile(filepath.Join(ioDir, "rc")); e == nil {
		if v, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
			rc = v
		}
	} else {
		// The container never wrote the rc file (it was killed before finishing).
		rc = -1
	}

	if timedOut || rc != 0 {
		stderrBytes, _ := os.ReadFile(filepath.Join(ioDir, "runtime.err"))
		stderr := strings.TrimSpace(string(stderrBytes))

		switch {
		case timedOut || rc == 124:
			result.Status = "time_limit_exceeded"
			result.Info = "Time limit exceeded"
		case rc == 137:
			// 137 = 128 + SIGKILL(9): usually the OOM killer (memory limit hit).
			result.Status = "memory_limit_exceeded"
			result.Info = "Memory limit exceeded"
		default:
			result.Status = "runtime_error"
			switch {
			case rc == 139:
				// 139 = 128 + SIGSEGV(11): segmentation fault, often stack overflow.
				result.Info = "Runtime error (segmentation fault / stack overflow, signal 11)"
			case rc < 0:
				result.Info = "Runtime error (program was terminated before completion)"
			default:
				result.Info = fmt.Sprintf("Runtime error (exit code %d)", rc)
			}
			if stderr != "" {
				result.Info += "\n" + stderr
			}
			if ob, oe := os.ReadFile(filepath.Join(ioDir, "out.out")); oe == nil && len(ob) > 0 {
				result.Info += "\nProgram output:\n" + string(ob)
			}
		}
		return result
	}

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
	gotBytes, _ := os.ReadFile(filepath.Join(ioDir, "out.out"))
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

	// Token-based comparison: split both outputs on any run of whitespace and
	// compare the resulting tokens in order. This is the standard OJ answer
	// comparison; it is insensitive to differences between spaces and newlines
	// and to trailing whitespace, while still catching any difference in the
	// actual values. (The previous char-by-char comparison mishandled the
	// space-vs-newline case and could wrongly report a correct answer as wrong.)
	gotTokens := strings.Fields(got)
	expTokens := strings.Fields(expected)

	if len(gotTokens) != len(expTokens) {
		result.Passed = false
		result.Status = "wrong_answer"
		if len(gotTokens) < len(expTokens) {
			result.Info = fmt.Sprintf("Output ended early: expected %d tokens but got %d (next expected '%s')",
				len(expTokens), len(gotTokens), expTokens[len(gotTokens)])
		} else {
			result.Info = fmt.Sprintf("Output too long: expected %d tokens but got %d",
				len(expTokens), len(gotTokens))
		}
		return result
	}

	for i := range expTokens {
		if gotTokens[i] != expTokens[i] {
			result.Passed = false
			result.Status = "wrong_answer"
			result.Info = fmt.Sprintf("Wrong answer at token %d: expected '%s', got '%s'",
				i+1, expTokens[i], gotTokens[i])
			return result
		}
	}

	result.Passed = true
	result.Status = "accepted"
	result.Info = "Accepted"
	return result
}

// runContainerDetached starts a Docker container in detached mode, waits for it to
// finish (up to timeout), and guarantees the container is removed.
//
// Why detached instead of exec.CommandContext with "docker run --rm": when the Go
// context deadline fires, exec.CommandContext sends SIGKILL to the docker CLI. That
// kills the client but the container keeps running inside the Docker daemon (the
// workload has no internal timeout), and because --rm only removes a container when
// *it* exits, a hung container is never removed. Over many submissions (e.g. TLE /
// infinite-loop solutions) these orphaned containers accumulate and leak memory in
// the Docker daemon. Running detached and always "docker rm -f" at the end ensures
// every container is cleaned up, so the daemon cannot leak containers/memory.
func runContainerDetached(dockerArgs []string, timeout time.Duration) (timedOut bool, err error) {
	// Build "docker run -d ..." so we obtain the container ID from stdout and can
	// forcibly remove it later if needed.
	startArgs := make([]string, 0, len(dockerArgs)+1)
	if len(dockerArgs) > 0 && dockerArgs[0] == "run" {
		startArgs = append(startArgs, "run", "-d")
		startArgs = append(startArgs, dockerArgs[1:]...)
	} else {
		startArgs = append(startArgs, "-d")
		startArgs = append(startArgs, dockerArgs...)
	}

	startCmd := exec.Command("docker", startArgs...)
	idOut, startErr := startCmd.Output()
	if startErr != nil {
		return false, fmt.Errorf("docker run failed: %w", startErr)
	}
	containerID := strings.TrimSpace(string(idOut))
	if containerID == "" {
		return false, fmt.Errorf("docker run returned empty container id")
	}

	// Always attempt to remove the container. On normal exit --rm already removed it
	// (docker's "rm -f" then returns a non-zero status — a benign CLI quirk we
	// ignore); on timeout or error this guarantees cleanup so the Docker daemon
	// never leaks the container and its memory. We intentionally ignore the error
	// because "rm -f" is meant to be best-effort: the container is either already
	// gone (--rm) or forcibly removed now.
	defer func() {
		_ = exec.Command("docker", "rm", "-f", containerID).Run()
	}()

	waitCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	waitCmd := exec.CommandContext(waitCtx, "docker", "wait", containerID)
	var waitOut bytes.Buffer
	waitCmd.Stdout = &waitOut
	waitCmd.Stderr = &waitOut
	if werr := waitCmd.Run(); werr != nil {
		if waitCtx.Err() == context.DeadlineExceeded {
			return true, nil
		}
		return false, fmt.Errorf("docker wait failed: %w", werr)
	}
	return false, nil
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

// runJudge compiles and runs all test cases for a submission and returns the
// resulting status and per-test results. It does NOT write to the database, so it
// can be reused by both the local judge loop and distributed workers.
func runJudge(sub sql_service.Submission) (status string, results []sql_service.TestResult) {
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
			return "internal_error", nil
		}
	}
	// try to make tmpDir world-readable/executable so other processes can inspect
	_ = os.Chmod(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	// write code file
	codePath := filepath.Join(tmpDir, "solution.cpp")

	if err := os.WriteFile(codePath, []byte(sub.Code), 0644); err != nil {
		log.Printf("failed to write code file: %v", err)
		return "internal_error", nil
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
			// time limit: accept time_limit (milliseconds, common in Chinese OI problems)
			// or time_limit_s/time_limit (seconds)
			if v, ok := obj["time_limit"].(float64); ok {
				// Most config files use milliseconds (1000 = 1 second)
				// If value > 100, assume it's in milliseconds
				if v > 100 {
					timeLimit = v / 1000.0
				} else {
					timeLimit = v
				}
			} else if v, ok := obj["time_limit_ms"].(float64); ok {
				timeLimit = v / 1000.0
			} else if v, ok := obj["time_limit_s"].(float64); ok {
				timeLimit = v
			}
			// memory: accept mem_mb or mem_limit_mb
			if v, ok := obj["memory_limit"].(float64); ok {
				memMB = int(v)
			} else if v, ok := obj["memory_limit_mb"].(float64); ok {
				memMB = int(v)
			}
		}
	}

	results = []sql_service.TestResult{}
	// Default; overridden below by the all-pass / not-accepted decision, or by an
	// early return. Kept as "accepted" (not the legacy "ok") so the stored status
	// is always consistent with what the web layer queries.
	status = "accepted"

	// compile inside docker
	// use absolute paths to avoid stray files
	absTmp, _ := filepath.Abs(tmpDir)
	// use absolute g++ path to avoid PATH issues inside image.
	// Redirect compiler output and exit code to files in the work dir so they
	// survive the detached run (a detached container's stdio is not captured).
	compileCmd := "g++ solution.cpp -O2 -std=c++17 -o solution > compile.err 2>&1; echo $? > compile.rc"
	// compilation can require significantly more memory than runtime limits; raise compile memory cap
	compileMem := 512
	dockerCompileArgs := []string{"run", "--rm", "-v", absTmp + ":/work", "-w", "/work", "--network", "none", "--memory", fmt.Sprintf("%dm", compileMem), "--cpus", "1.0", "--ulimit", fmt.Sprintf("stack=%d", compileMem*1024*1024), "gcc-with-time", "bash", "-lc", fmt.Sprintf("%v", compileCmd)}
	// increase compile timeout to allow for image/pulled layers and heavier builds;
	// run detached so a timeout cannot leak the container (see runContainerDetached).
	compileTimedOut, compileErr := runContainerDetached(dockerCompileArgs, 10*time.Second)
	if compileErr != nil || compileTimedOut {
		status = "compile_error"
		var outStr string
		if compileTimedOut {
			outStr = "compilation timed out"
		} else {
			outStr = fmt.Sprintf("compilation failed to start: %v", compileErr)
		}
		if eb, e := os.ReadFile(filepath.Join(absTmp, "compile.err")); e == nil {
			outStr += "\n" + string(eb)
		}
		results = append(results, sql_service.TestResult{TestIndex: 0, Passed: false, Output: outStr, TimeMs: 0, MemoryKB: 0})
		return status, results
	}

	// read compile result written by the container
	rcBytes, _ := os.ReadFile(filepath.Join(absTmp, "compile.rc"))
	compileRC, _ := strconv.Atoi(strings.TrimSpace(string(rcBytes)))
	if compileRC != 0 {
		status = "compile_error"
		eb, _ := os.ReadFile(filepath.Join(absTmp, "compile.err"))
		outStr := string(eb)
		results = append(results, sql_service.TestResult{TestIndex: 0, Passed: false, Output: outStr, TimeMs: 0, MemoryKB: 0})
		return status, results
	}

	// Run every test case of the submission concurrently using a bounded pool of
	// goroutines (single-task multi-threaded evaluation). Each test gets its own
	// isolated work subdirectory so the Docker containers never clobber each
	// other's in.in / out.out / time.log / rc files.
	//
	// Group semantics are preserved: each test group carries a score that is
	// credited to the group's last test case only when *all* tests in the group
	// pass; the submission is accepted only when every group passes. (The previous
	// "skip the rest of a group after a failure" optimization is dropped — with
	// concurrency every test is evaluated independently and its true result is
	// shown, which is strictly more informative.)

	obj := make(map[string]any)
	_ = json.Unmarshal(cfgData, &obj)
	testGroups := []interface{}{}
	if v, ok := obj["test_cases"].([]interface{}); ok {
		testGroups = v
	} else {
		// return error if test groups not found; we require test groups to determine how many tests to run
		return "internal_error", nil
	}

	// Parse groups into a flat list of jobs while remembering which slot belongs to
	// which group, so group scoring can be applied after all tests finish.
	type parsedGroup struct {
		score int
		tests []int
	}
	type testJob struct {
		groupIdx int
		testIdx  int
		slot     int
	}

	groups := make([]parsedGroup, 0, len(testGroups))
	jobs := make([]testJob, 0)
	groupSlots := make([][]int, 0, len(testGroups))

	for _, tg := range testGroups {
		tgMap, ok := tg.(map[string]any)
		if !ok {
			continue
		}
		pg := parsedGroup{}
		if v, ok := tgMap["score"].(float64); ok { // tolerate a missing score (defaults to 0)
			pg.score = int(v)
		}
		if v, ok := tgMap["cases"].([]interface{}); ok {
			for _, cv := range v {
				if num, ok := cv.(float64); ok {
					pg.tests = append(pg.tests, int(num))
				}
			}
		}
		if len(pg.tests) == 0 {
			continue
		}
		gIdx := len(groups)
		groups = append(groups, pg)
		slots := make([]int, 0, len(pg.tests))
		for _, ti := range pg.tests {
			slot := len(jobs)
			jobs = append(jobs, testJob{groupIdx: gIdx, testIdx: ti, slot: slot})
			slots = append(slots, slot)
		}
		groupSlots = append(groupSlots, slots)
	}

	if len(jobs) == 0 {
		return "internal_error", nil
	}

	results = make([]sql_service.TestResult, len(jobs))

	// Bounded concurrency: cap simultaneous Docker containers so a submission with
	// many test cases cannot exhaust host memory. The limit comes from
	// judge.per_task_concurrency (config); it is also capped at the job count.
	perTask := config.GetPerTaskConcurrency()
	if perTask < 1 {
		perTask = len(jobs)
	}
	if perTask > len(jobs) {
		perTask = len(jobs)
	}
	sem := make(chan struct{}, perTask)
	var wg sync.WaitGroup

	for _, job := range jobs {
		wg.Add(1)
		go func(j testJob) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			inPath := filepath.Join("data", "problem", fmt.Sprintf("%d", sub.ProblemID), fmt.Sprintf("%d.in", j.testIdx))
			expectedPath := filepath.Join("data", "problem", fmt.Sprintf("%d", sub.ProblemID), fmt.Sprintf("%d.ans", j.testIdx))

			// The compiled `solution` binary lives in the parent tmpDir (mounted as
			// /work). We isolate each test's I/O in its own subdir via TestIODir so
			// concurrently running containers never overwrite each other's
			// in.in / out.out / time.log / rc files.
			cfg := JudgeConfig{
				TimeLimit:    timeLimit,
				MemLimit:     memMB,
				InputPath:    inPath,
				WorkTmpPath:  tmpDir,
				TestIODir:    fmt.Sprintf("tc-%d", j.slot),
				ExpectedPath: expectedPath,
			}

			r := JudgeTest(cfg)
			results[j.slot] = sql_service.TestResult{
				TestIndex: j.testIdx,
				Passed:    r.Passed,
				Output:    r.Info, // for WA, Info holds the mismatch details; for RE, the error message
				TimeMs:    r.RunTimeMs,
				MemoryKB:  r.MemoryKB,
				Status:    r.Status,
				Score:     0,
			}
		}(job)
	}
	wg.Wait()

	// Apply group scoring and compute the overall submission status.
	allPassed := true
	for gi, g := range groups {
		groupPassed := true
		for _, slot := range groupSlots[gi] {
			if !results[slot].Passed {
				groupPassed = false
				allPassed = false
			}
		}
		if groupPassed && len(groupSlots[gi]) > 0 {
			results[groupSlots[gi][len(groupSlots[gi])-1]].Score = g.score
		}
	}

	if allPassed {
		status = "accepted"
	} else {
		status = "not accepted"
	}

	return status, results
}

// processJob runs the judge and writes the result to the database. Used by the
// local judge loop and by the coordinator's embedded judge.
func processJob(sub sql_service.Submission) {
	status, results := runJudge(sub)
	if status == "" {
		status = "internal_error"
	}
	_ = sql_service.UpdateSubmissionResult(sub.ID, status, results)
	appendMessage(fmt.Sprintf("%s submitted %d => %s", sub.Username, sub.ProblemID, status))
}
