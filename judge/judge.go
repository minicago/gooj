package judge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/minicago/gooj/file_service"
)

type Job struct {
	Username string `json:"username"`
	Problem  string `json:"problem"`
	Code     string `json:"code"`
}

// StartJudge starts the judge loop as a goroutine. It processes jobs from data/queue.json
func StartJudge() {
	svc := file_service.Default()
	if svc == nil {
		log.Printf("file service not started; judge not running")
		return
	}
	queuePath := filepath.Join("data", "queue.json")

	go func() {
		for {
			// atomically pop one job
			var popped Job
			_, err := svc.ModifyFile(queuePath, func(cur []byte) ([]byte, error) {
				var arr []Job
				if len(cur) > 0 {
					if err := json.Unmarshal(cur, &arr); err != nil {
						arr = []Job{}
					}
				}
				if len(arr) == 0 {
					// nothing to do
					return cur, nil
				}
				popped = arr[0]
				outArr := arr[1:]
				out, err := json.Marshal(outArr)
				if err != nil {
					return cur, err
				}
				return out, nil
			})
			if err != nil {
				log.Printf("judge: queue modify error: %v", err)
				time.Sleep(time.Second)
				continue
			}
			// if nothing popped, sleep
			if popped.Username == "" {
				time.Sleep(time.Second)
				continue
			}

			// process popped job
			processJob(svc, popped)
		}
	}()
}

func processJob(svc *file_service.Service, job Job) {
	userDir := filepath.Join("data", "user", job.Username)
	codeBase := filepath.Base(job.Code)
	// read problem config
	cfgPath := filepath.Join("data", "problem", job.Problem, "config.json")
	cfgData, _ := svc.ReadFile(cfgPath)
	tests := 1
	timeLimit := 5.0
	memMB := 256
	if len(cfgData) > 0 {
		var obj map[string]any
		if err := json.Unmarshal(cfgData, &obj); err == nil {
			if v, ok := obj["tests"].(float64); ok {
				tests = int(v)
			}
			if v, ok := obj["time_limit"].(float64); ok {
				timeLimit = v
			}
			if v, ok := obj["mem_mb"].(float64); ok {
				memMB = int(v)
			}
		}
	}

	resultPath := filepath.Join(userDir, job.Problem+".result")

	// compile once per submission
	// write code is already in userDir; compile inside docker
	absUserDir, _ := filepath.Abs(userDir)
	compileCmd := fmt.Sprintf("g++ %s -O2 -std=c++17 -o solution 2>compile.err; if [ -s compile.err ]; then cat compile.err >&2; exit 2; fi", codeBase)
	dockerCompileArgs := []string{"run", "--rm", "-v", absUserDir + ":/work", "-w", "/work", "--network", "none", "--memory", fmt.Sprintf("%dm", memMB), "--cpus", "0.5", "gcc:12", "bash", "-lc", compileCmd}
	cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer ccancel()
	ccmd := exec.CommandContext(cctx, "docker", dockerCompileArgs...)
	var cerr bytes.Buffer
	ccmd.Stderr = &cerr
	if err := ccmd.Run(); err != nil {
		// compile error
		svc.WriteFile(resultPath, []byte("compile error:\n"+cerr.String()))
		svc.ModifyFile(filepath.Join("data", "message.txt"), func(cur []byte) ([]byte, error) {
			newline := []byte(fmt.Sprintf("%s submitted %s => COMPILE_ERROR\n", job.Username, job.Problem))
			return append(cur, newline...), nil
		})
		return
	}

	// run tests sequentially
	for i := 1; i <= tests; i++ {
		inPath := filepath.Join("data", "problem", job.Problem, fmt.Sprintf("%d.in", i))
		expectedPath := filepath.Join("data", "problem", job.Problem, fmt.Sprintf("%d.out", i))
		// copy test input
		if b, err := svc.ReadFile(inPath); err == nil {
			_ = svc.WriteFile(filepath.Join(userDir, "in.in"), b)
		} else {
			_ = svc.WriteFile(filepath.Join(userDir, "in.in"), []byte(""))
		}

		// run inside docker with timeout via 'timeout' utility
		shellCmd := fmt.Sprintf("timeout %ds ./solution < in.in > out.out 2>runtime.err; if [ -s runtime.err ]; then cat runtime.err >&2; exit 3; fi; cat out.out", int(timeLimit))
		dockerArgs := []string{"run", "--rm", "-v", absUserDir + ":/work", "-w", "/work", "--network", "none", "--memory", fmt.Sprintf("%dm", memMB), "--pids-limit", "64", "--cpu-shares", "128", "gcc:12", "bash", "-lc", shellCmd}

		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeLimit+5)*time.Second)
		cmd := exec.CommandContext(ctx, "docker", dockerArgs...)
		var outb bytes.Buffer
		var errb bytes.Buffer
		cmd.Stdout = &outb
		cmd.Stderr = &errb
		err := cmd.Run()
		cancel()

		if err != nil {
			// determine exit code
			exitCode := -1
			if ee, ok := err.(*exec.ExitError); ok {
				exitCode = ee.ExitCode()
			} else if strings.Contains(err.Error(), "context deadline exceeded") {
				// treat as TLE
				exitCode = 124
			}

			stderr := errb.String()
			if exitCode == 124 {
				svc.WriteFile(resultPath, []byte(fmt.Sprintf("TLE on test %d\n", i)))
				svc.ModifyFile(filepath.Join("data", "message.txt"), func(cur []byte) ([]byte, error) {
					newline := []byte(fmt.Sprintf("%s submitted %s => TLE\n", job.Username, job.Problem))
					return append(cur, newline...), nil
				})
				return
			}
			if exitCode == 137 {
				svc.WriteFile(resultPath, []byte(fmt.Sprintf("MLE on test %d\n", i)))
				svc.ModifyFile(filepath.Join("data", "message.txt"), func(cur []byte) ([]byte, error) {
					newline := []byte(fmt.Sprintf("%s submitted %s => MLE\n", job.Username, job.Problem))
					return append(cur, newline...), nil
				})
				return
			}
			if exitCode == 3 {
				svc.WriteFile(resultPath, []byte(fmt.Sprintf("RUNTIME_ERROR on test %d:\n%s", i, stderr)))
				svc.ModifyFile(filepath.Join("data", "message.txt"), func(cur []byte) ([]byte, error) {
					newline := []byte(fmt.Sprintf("%s submitted %s => RUNTIME_ERROR\n", job.Username, job.Problem))
					return append(cur, newline...), nil
				})
				return
			}
			// other docker error
			svc.WriteFile(resultPath, []byte(fmt.Sprintf("docker/run error on test %d: %v\n%s", i, err, stderr)))
			svc.ModifyFile(filepath.Join("data", "message.txt"), func(cur []byte) ([]byte, error) {
				newline := []byte(fmt.Sprintf("%s submitted %s => RUNTIME_ERROR\n", job.Username, job.Problem))
				return append(cur, newline...), nil
			})
			return
		}

		// compare output
		expected, _ := svc.ReadFile(expectedPath)
		got := outb.Bytes()
		normalize := func(b []byte) string {
			s := string(b)
			s = strings.ReplaceAll(s, "\r\n", "\n")
			s = strings.TrimSpace(s)
			return s
		}
		if normalize(got) != normalize(expected) {
			resText := fmt.Sprintf("WA on test %d\n--- expected ---\n%s\n--- got ---\n%s", i, string(expected), string(got))
			svc.WriteFile(resultPath, []byte(resText))
			svc.ModifyFile(filepath.Join("data", "message.txt"), func(cur []byte) ([]byte, error) {
				newline := []byte(fmt.Sprintf("%s submitted %s => WA\n", job.Username, job.Problem))
				return append(cur, newline...), nil
			})
			return
		}
		// else this test passed, continue
	}

	// all tests passed
	svc.WriteFile(resultPath, []byte("OK"))
	svc.ModifyFile(filepath.Join("data", "message.txt"), func(cur []byte) ([]byte, error) {
		newline := []byte(fmt.Sprintf("%s submitted %s => OK\n", job.Username, job.Problem))
		return append(cur, newline...), nil
	})
}
