package judge

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	sandboxIOImage       = "go-judge-sandbox:latest" // built by `make sandbox-image`
	compileMemoryLimit   = "512m"
	compileTimeout       = 30 * time.Second
	runOverheadPerTest   = time.Second // process start + harness bookkeeping
	runContainerOverhead = 10 * time.Second
	ioSandboxUser        = "65534:65534" // nobody: untrusted code must not own anything it can reach
	ioPidsLimit          = "128"
	minMemoryLimitMB     = 16 // below this the Python interpreter alone gets OOM-killed
)

var (
	// ErrInvalidIORequest is a bad problem configuration, not a bad submission.
	ErrInvalidIORequest = errors.New("invalid I/O request")
	// ErrHarnessTampered means the harness's result lines can't be trusted:
	// user code wrote to the harness's stdout (it runs as the same user).
	ErrHarnessTampered = errors.New("harness output tampered")
)

type Language string

const (
	LangCPP17   Language = "cpp17"
	LangPython3 Language = "python3"
)

type langSpec struct {
	source  string
	compile []string
	run     []string
}

// ponytail: languages live in code; move to a table when they need to change without a deploy
var languages = map[Language]langSpec{
	LangCPP17: {
		source:  "main.cpp",
		compile: []string{"g++", "-O2", "-std=c++17", "-o", "/box/bin/main", "/box/src/main.cpp"},
		run:     []string{"/box/bin/main"},
	},
	LangPython3: {
		source:  "main.py",
		compile: []string{"python3", "-m", "py_compile", "/box/src/main.py"},
		run:     []string{"python3", "-B", "/box/src/main.py"},
	},
}

type IORequest struct {
	Language      Language
	Code          string
	Tests         []TestCase
	Subtasks      []Subtask
	TimeLimit     time.Duration
	MemoryLimitMB int
}

type IOResult struct {
	Verdict       Verdict
	CompileOutput string
	Tests         []TestResult
	Score         int
}

func (r Runner) RunIO(ctx context.Context, req IORequest) (IOResult, error) {
	spec, ok := languages[req.Language]
	if !ok {
		return IOResult{}, fmt.Errorf("unsupported language %q", req.Language)
	}
	if err := validateIORequest(req); err != nil {
		return IOResult{}, err
	}

	dir, err := os.MkdirTemp("", "submission_*")
	if err != nil {
		return IOResult{}, err
	}
	defer os.RemoveAll(dir)

	if err := writeBox(dir, spec.source, req); err != nil {
		return IOResult{}, err
	}

	compileOut, ok, err := r.compile(ctx, dir, spec)
	if err != nil {
		return IOResult{}, err
	}
	if !ok {
		return IOResult{Verdict: VerdictCE, CompileOutput: compileOut}, nil
	}

	runs, err := r.runTests(ctx, dir, spec, req)
	if err != nil {
		return IOResult{}, err
	}

	results := make([]TestResult, len(req.Tests))
	for i, tc := range req.Tests {
		run := runs[i]
		v := classifyRun(run, req.TimeLimit)
		if v == VerdictAC {
			out, err := os.ReadFile(filepath.Join(dir, "out", strconv.Itoa(i)+".out"))
			if err != nil {
				return IOResult{}, err
			}
			if !tokensEqual(string(out), tc.Answer) {
				v = VerdictWA
			}
		}
		results[i] = TestResult{Verdict: v, TimeMs: int(run.Elapsed.Milliseconds()), MemoryKB: run.MemoryKB}
	}

	return IOResult{
		Verdict:       overallVerdict(results),
		CompileOutput: compileOut,
		Tests:         results,
		Score:         score(req.Tests, results, req.Subtasks),
	}, nil
}

// validateIORequest checks the problem configuration. It comes from the DB or
// the AI generator, so it's a trust boundary like user code.
func validateIORequest(req IORequest) error {
	if req.TimeLimit <= 0 {
		return fmt.Errorf("%w: time limit must be positive, got %s", ErrInvalidIORequest, req.TimeLimit)
	}
	if req.MemoryLimitMB < minMemoryLimitMB {
		return fmt.Errorf("%w: memory limit must be at least %dMB, got %d", ErrInvalidIORequest, minMemoryLimitMB, req.MemoryLimitMB)
	}
	if len(req.Tests) == 0 {
		return fmt.Errorf("%w: no tests", ErrInvalidIORequest)
	}

	testsPerSubtask := map[int]int{}
	for _, tc := range req.Tests {
		testsPerSubtask[tc.Subtask]++
	}
	defined := map[int]bool{0: true} // 0 = samples
	for _, st := range req.Subtasks {
		defined[st.Index] = true
		if testsPerSubtask[st.Index] == 0 {
			return fmt.Errorf("%w: subtask %d has no tests, its points would be free", ErrInvalidIORequest, st.Index)
		}
	}
	for i, tc := range req.Tests {
		if !defined[tc.Subtask] {
			return fmt.Errorf("%w: test %d is in undefined subtask %d", ErrInvalidIORequest, i, tc.Subtask)
		}
	}
	return nil
}

func writeBox(dir, source string, req IORequest) error {
	// world-writable/readable: the container runs as nobody, the dir is owned by us
	for _, sub := range []string{"src", "bin", "tests", "out"} {
		p := filepath.Join(dir, sub)
		if err := os.Mkdir(p, 0o777); err != nil {
			return err
		}
		if err := os.Chmod(p, 0o777); err != nil { // Mkdir is subject to umask
			return err
		}
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "src", source), []byte(req.Code), 0o644); err != nil {
		return err
	}
	for i, tc := range req.Tests {
		if err := os.WriteFile(filepath.Join(dir, "tests", strconv.Itoa(i)+".in"), []byte(tc.Input), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (r Runner) compile(ctx context.Context, dir string, spec langSpec) (output string, ok bool, err error) {
	compileCtx, cancel := context.WithTimeout(ctx, compileTimeout)
	defer cancel()

	name := "gojudge-compile-" + filepath.Base(dir)
	args := append(r.sandboxArgs(name, compileMemoryLimit,
		"-v", filepath.Join(dir, "src")+":/box/src",
		"-v", filepath.Join(dir, "bin")+":/box/bin",
	), spec.compile...)

	out, runErr := dockerRun(compileCtx, name, args)
	output = strings.TrimSpace(out.buf.String())
	if out.truncated {
		output += fmt.Sprintf("\n... output truncated: exceeded the %d byte limit", maxOutputBytes)
	}

	switch {
	case ctx.Err() != nil: // the caller gave up, there's no verdict to report
		return "", false, ctx.Err()
	case compileCtx.Err() != nil:
		return "compilation timed out", false, nil
	case isSandboxFailure(runErr):
		return "", false, fmt.Errorf("%w: compile: %v: %s", ErrSandboxUnavailable, runErr, output)
	case runErr != nil:
		return output, false, nil // the compiler rejected the code
	}
	return output, true, nil
}

func (r Runner) runTests(ctx context.Context, dir string, spec langSpec, req IORequest) (map[int]harnessRun, error) {
	budget := time.Duration(len(req.Tests))*(req.TimeLimit+runOverheadPerTest) + runContainerOverhead
	runCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	name := "gojudge-run-" + filepath.Base(dir)
	mem := strconv.Itoa(req.MemoryLimitMB) + "m"
	args := r.sandboxArgs(name, mem,
		"--memory-swap", mem, // no swap, otherwise MLE turns into slow AC
		"-v", filepath.Join(dir, "src")+":/box/src:ro",
		"-v", filepath.Join(dir, "bin")+":/box/bin:ro",
		"-v", filepath.Join(dir, "tests")+":/box/tests:ro",
		"-v", filepath.Join(dir, "out")+":/box/out",
	)
	args = append(args, "judge-harness", strconv.FormatFloat(req.TimeLimit.Seconds(), 'f', -1, 64))
	args = append(args, spec.run...)

	out, err := dockerRun(runCtx, name, args)
	switch {
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case isSandboxFailure(err):
		return nil, fmt.Errorf("%w: run: %v: %s", ErrSandboxUnavailable, err, out.buf.String())
	case err != nil:
		return nil, fmt.Errorf("run sandbox: %w: %s", err, out.buf.String())
	case out.truncated:
		// the harness prints one short line per test, so this much output is user code
		return nil, fmt.Errorf("%w: %d+ bytes of harness output", ErrHarnessTampered, maxOutputBytes)
	}
	return parseHarnessOutput(out.buf.Bytes(), len(req.Tests))
}

// parseHarnessOutput expects exactly one line per test. The harness always
// prints the real line (PID 1 can't be killed from inside the container), so a
// duplicate or unknown index can only come from user code writing to
// /proc/1/fd/1.
func parseHarnessOutput(out []byte, tests int) (map[int]harnessRun, error) {
	runs := map[int]harnessRun{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		run, err := parseHarnessLine(sc.Text())
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrHarnessTampered, err)
		}
		if run.Index < 0 || run.Index >= tests {
			return nil, fmt.Errorf("%w: result for unknown test %d", ErrHarnessTampered, run.Index)
		}
		if _, dup := runs[run.Index]; dup {
			return nil, fmt.Errorf("%w: two results for test %d", ErrHarnessTampered, run.Index)
		}
		runs[run.Index] = run
	}
	if len(runs) != tests {
		return nil, fmt.Errorf("%w: got results for %d of %d tests", ErrHarnessTampered, len(runs), tests)
	}
	return runs, nil
}

func (r Runner) sandboxArgs(name, memory string, extra ...string) []string {
	args := []string{
		"run", "--rm",
		"--pull", "never",
		"--name", name,
		"--network", "none",
		"--memory", memory,
		"--cpus", "1", // wall-clock time limits assume one full core
		"--pids-limit", ioPidsLimit,
		"--read-only",
		"--tmpfs", "/tmp:size=64m",
		"--user", ioSandboxUser,
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"-e", "HOME=/tmp",
		"-w", "/box",
	}
	args = append(args, extra...)
	return append(args, r.ioImage)
}

func dockerRun(ctx context.Context, name string, args []string) (*cappedBuffer, error) {
	defer func() {
		killCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = exec.CommandContext(killCtx, "docker", "rm", "-f", name).Run()
	}()

	out := &cappedBuffer{max: maxOutputBytes}
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = out
	cmd.Stderr = out
	return out, cmd.Run()
}
