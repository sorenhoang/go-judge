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
		"go.mod": `module solution

go 1.26
`,
		"solution.go":      "package solution\n\n" + code + "\n",
		"solution_test.go": "package solution\n\nimport \"testing\"\n\n" + testCode + "\n",
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			return Result{}, err
		}
	}

	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(testCtx, "go", "test", "-json", "./...")
	cmd.Dir = dir

	output, err := cmd.CombinedOutput()
	result := parseGoTestOutput(output)

	result.Output = strings.TrimSpace(string(output))

	if errors.Is(testCtx.Err(), context.DeadlineExceeded) {
		result.Verdict = submission.StatusError
		if result.Output == "" {
			result.Output = "go test timed out"
		}
		return result, nil
	}

	if result.TotalTests == 0 {
		result.Verdict = submission.StatusError
		if result.Output == "" && err != nil {
			result.Output = "go test failed: " + err.Error()
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
