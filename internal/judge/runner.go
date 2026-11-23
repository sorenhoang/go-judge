package judge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sorenhoang/go-judge/internal/submission"
)

const (
	sandboxImage       = "gojudge-gotest:latest" // built from sandbox/gotest.Dockerfile, stdlib pre-compiled
	sandboxMemoryLimit = "512m"
	sandboxCPULimit    = "0.5"
	sandboxPidsLimit   = "128"
	sandboxTmpfsSize   = "512m,exec" // exec: tmpfs is noexec by default, which blocks running the compiled test binary
	sandboxTimeout     = 30 * time.Second
	goTestSandboxUser  = "65534:65534" // nobody; the image chowns /gocache to it
	maxOutputBytes     = 1 << 20       // well under gRPC's 4MB default receive limit
	dockerErrorExit    = 125           // `docker run` itself failed (daemon down, image missing, bad flags)
)

// ErrSandboxUnavailable means the sandbox couldn't run at all, so the
// submission never got judged. Callers should retry, not blame the user.
var ErrSandboxUnavailable = errors.New("sandbox unavailable")

type Result struct {
	Verdict     submission.Status
	Output      string
	TotalTests  int
	PassedTests int
}

type Runner struct {
	image   string // go test problems
	ioImage string // stdin/stdout problems
}

func NewRunner() Runner {
	return Runner{image: sandboxImage, ioImage: sandboxIOImage}
}

func (r Runner) Run(ctx context.Context, code, testCode string) (Result, error) {
	dir, err := os.MkdirTemp("", "submission_*")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(dir)

	// the sandbox runs as nobody, so it needs read access to the host-side files
	if err := os.Chmod(dir, 0755); err != nil {
		return Result{}, err
	}

	files := map[string]string{
		"go.mod":           "module solution\n\ngo 1.21\n", // low on purpose so it's not fragile to the image's Go version
		"solution.go":      "package solution\n\n" + code + "\n",
		"solution_test.go": "package solution\n\nimport \"testing\"\n\n" + testCode + "\n",
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			return Result{}, err
		}
	}

	testCtx, cancel := context.WithTimeout(ctx, sandboxTimeout)
	defer cancel()

	containerName := "gojudge-" + filepath.Base(dir)

	args := []string{
		"run", "--rm",
		"--pull", "never",
		"--name", containerName,
		"--network", "none",
		"--memory", sandboxMemoryLimit,
		"--cpus", sandboxCPULimit,
		"--pids-limit", sandboxPidsLimit,
		"--read-only",
		"--user", goTestSandboxUser,
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--tmpfs", "/tmp:size=" + sandboxTmpfsSize,
		"-v", "/gocache", // anonymous volume, seeded from the image's warm cache, removed by --rm
		"-e", "GOTMPDIR=/tmp",
		"-v", dir + ":/app:ro",
		"-w", "/app",
		r.image,
		"go", "test", "-json", "./...",
	}

	cmd := exec.CommandContext(testCtx, "docker", args...)
	out := &cappedBuffer{max: maxOutputBytes}
	cmd.Stdout = out
	cmd.Stderr = out

	// belt-and-suspenders: --rm handles the normal case, this handles the
	// "CLI got killed by testCtx, container kept running" case. Error is
	// expected/harmless when the container is already gone.
	defer func() {
		killCtx, killCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer killCancel()
		_ = exec.CommandContext(killCtx, "docker", "rm", "-f", containerName).Run()
	}()

	runErr := cmd.Run()
	output := strings.TrimSpace(out.buf.String())

	// the caller gave up (worker timeout / shutdown); there's no verdict to report
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}

	if errors.Is(testCtx.Err(), context.DeadlineExceeded) {
		if output == "" {
			output = "sandbox timed out"
		}
		return Result{Verdict: submission.StatusError, Output: output}, nil
	}

	if isSandboxFailure(runErr) {
		return Result{}, fmt.Errorf("%w: %v: %s", ErrSandboxUnavailable, runErr, output)
	}

	result := parseGoTestOutput(out.buf.Bytes())
	result.Output = output

	if out.truncated {
		result.Verdict = submission.StatusError
		result.Output += fmt.Sprintf("\n... output truncated: exceeded the %d byte limit", maxOutputBytes)
		return result, nil
	}

	result.Verdict = decideVerdict(result, runErr == nil)
	if result.Output == "" && runErr != nil {
		result.Output = "sandbox failed: " + runErr.Error()
	}

	return result, nil
}

// decideVerdict never trusts the parsed events alone: PASSED also needs `go test`
// to exit 0, so a crashed or killed run can't be reported as a pass.
func decideVerdict(r Result, exitOK bool) submission.Status {
	switch {
	case r.TotalTests == 0:
		return submission.StatusError
	case r.PassedTests < r.TotalTests:
		return submission.StatusFailed
	case !exitOK:
		return submission.StatusError
	default:
		return submission.StatusPassed
	}
}

func isSandboxFailure(err error) bool {
	if err == nil {
		return false
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return true // docker binary couldn't be started at all
	}
	return exitErr.ExitCode() == dockerErrorExit
}

// cappedBuffer keeps the first max bytes and silently drops the rest, so a
// submission that prints in a loop can't exhaust runner memory.
type cappedBuffer struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if room := b.max - b.buf.Len(); room < n {
		b.truncated = true
		p = p[:max(room, 0)]
	}
	b.buf.Write(p)
	return n, nil
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
