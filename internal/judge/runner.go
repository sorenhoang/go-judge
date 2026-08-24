package judge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sorenhoang/go-judge/internal/submission"
)

const (
	sandboxImage       = "golang:1.25" // pick whatever `docker pull` gets you; go.mod directive below is intentionally low so it's not fragile to this
	sandboxMemoryLimit = "512m"        // 128m OOM-kills the compiler on a from-scratch stdlib build (no cache persists between submissions)
	sandboxCPULimit    = "0.5"
	sandboxPidsLimit   = "128"
	sandboxTmpfsSize   = "512m,exec" // exec: tmpfs is noexec by default, which blocks running the compiled test binary
	sandboxTimeout     = 30 * time.Second
)

type Result struct {
	Verdict     submission.Status
	Output      string
	TotalTests  int
	PassedTests int
}

type Runner struct{}

func NewRunner() Runner {
	return Runner{}
}

func (r Runner) Run(ctx context.Context, code, testCode string) (Result, error) {
	dir, err := os.MkdirTemp("", "submission_*")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(dir)

	files := map[string]string{
		"go.mod":           "module solution\n\ngo 1.21\n", // low on purpose — see gotcha #2
		"solution.go":      "package solution\n\n" + code + "\n",
		"solution_test.go": "package solution\n\nimport \"testing\"\n\n" + testCode + "\n",
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			return Result{}, err
		}
	}

	testCtx, cancel := context.WithTimeout(ctx, sandboxTimeout)
	defer cancel()

	containerName := "gojudge-" + filepath.Base(dir)

	args := []string{
		"run", "--rm",
		"--name", containerName,
		"--network", "none",
		"--memory", sandboxMemoryLimit,
		"--cpus", sandboxCPULimit,
		"--pids-limit", sandboxPidsLimit,
		"--read-only",
		"--tmpfs", "/tmp:size=" + sandboxTmpfsSize,
		"-e", "GOCACHE=/tmp/gocache",
		"-e", "GOTMPDIR=/tmp",
		"-e", "GOTOOLCHAIN=local",
		"-v", dir + ":/app",
		"-w", "/app",
		sandboxImage,
		"go", "test", "-json", "./...",
	}

	cmd := exec.CommandContext(testCtx, "docker", args...)

	// belt-and-suspenders: --rm handles the normal case, this handles the
	// "CLI got killed by testCtx, container kept running" case. Error is
	// expected/harmless when the container is already gone.
	defer func() {
		killCtx, killCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer killCancel()
		_ = exec.CommandContext(killCtx, "docker", "rm", "-f", containerName).Run()
	}()

	output, err := cmd.CombinedOutput()
	result := parseGoTestOutput(output)

	result.Output = strings.TrimSpace(string(output))

	if errors.Is(testCtx.Err(), context.DeadlineExceeded) {
		result.Verdict = submission.StatusError
		if result.Output == "" {
			result.Output = "sandbox timed out"
		}
		return result, nil
	}

	if result.TotalTests == 0 {
		result.Verdict = submission.StatusError
		if result.Output == "" && err != nil {
			result.Output = "sandbox failed: " + err.Error()
		}
		return result, nil
	}

	if result.TotalTests > 0 && result.PassedTests == result.TotalTests {
		result.Verdict = submission.StatusPassed
		return result, nil
	}

	result.Verdict = submission.StatusFailed

	_ = err

	return result, nil
}

type goTestEvent struct {
	Action string `json:"Action"`
	Test   string `json:"Test"`
}

func parseGoTestOutput(output []byte) Result {
	decoder := json.NewDecoder(bytes.NewReader(output))

	passedTests := 0
	failedTests := 0

	for {
		var event goTestEvent
		if err := decoder.Decode(&event); err != nil {
			break
		}

		if event.Test == "" {
			continue
		}

		switch event.Action {
		case "pass":
			passedTests++
		case "fail":
			failedTests++
		}
	}

	return Result{
		TotalTests:  passedTests + failedTests,
		PassedTests: passedTests,
	}
}
