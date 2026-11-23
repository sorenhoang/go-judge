package judge

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sorenhoang/go-judge/internal/submission"
)

func TestDecideVerdict(t *testing.T) {
	cases := []struct {
		name   string
		result Result
		exitOK bool
		want   submission.Status
	}{
		{"all pass, clean exit", Result{TotalTests: 2, PassedTests: 2}, true, submission.StatusPassed},
		{"some fail", Result{TotalTests: 2, PassedTests: 1}, false, submission.StatusFailed},
		{"no tests ran (compile error)", Result{}, false, submission.StatusError},
		{"all pass but go test exited non-zero", Result{TotalTests: 1, PassedTests: 1}, false, submission.StatusError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := decideVerdict(tc.result, tc.exitOK); got != tc.want {
				t.Fatalf("decideVerdict = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestCappedBufferTruncates(t *testing.T) {
	b := cappedBuffer{max: 5}

	n, err := b.Write([]byte("abc"))
	if n != 3 || err != nil {
		t.Fatalf("Write = %d, %v", n, err)
	}
	// reports the full length so the writer side keeps going instead of erroring
	n, err = b.Write([]byte("defgh"))
	if n != 5 || err != nil {
		t.Fatalf("Write = %d, %v", n, err)
	}

	if got := b.buf.String(); got != "abcde" {
		t.Fatalf("buf = %q, want %q", got, "abcde")
	}
	if !b.truncated {
		t.Fatal("truncated = false, want true")
	}
}

func TestSmokeSandbox(t *testing.T) {
	if testing.Short() {
		t.Skip("needs a Docker daemon and the gojudge-sandbox image")
	}

	r := NewRunner()
	sumTest := `func TestSum(t *testing.T) { if Sum([]int{1,2,3}) != 6 { t.Fatal("wrong") } }`

	t.Run("passing", func(t *testing.T) {
		code := `func Sum(nums []int) int { total := 0; for _, n := range nums { total += n }; return total }`
		start := time.Now()
		res, err := r.Run(context.Background(), code, sumTest)
		t.Logf("elapsed=%s verdict=%s passed=%d total=%d err=%v output=%s", time.Since(start), res.Verdict, res.PassedTests, res.TotalTests, err, res.Output)
		if res.Verdict != submission.StatusPassed {
			t.Fatalf("expected PASSED, got %s", res.Verdict)
		}
	})

	t.Run("failing", func(t *testing.T) {
		code := `func Sum(nums []int) int { return 0 }`
		res, err := r.Run(context.Background(), code, sumTest)
		t.Logf("verdict=%s passed=%d total=%d err=%v output=%s", res.Verdict, res.PassedTests, res.TotalTests, err, res.Output)
		if res.Verdict != submission.StatusFailed {
			t.Fatalf("expected FAILED, got %s", res.Verdict)
		}
	})

	t.Run("timeout_and_cleanup", func(t *testing.T) {
		code := `func Sum(nums []int) int { for {} }`
		testCode := `func TestSum(t *testing.T) { Sum([]int{1}) }`
		start := time.Now()
		res, err := r.Run(context.Background(), code, testCode)
		t.Logf("elapsed=%s verdict=%s err=%v output=%s", time.Since(start), res.Verdict, err, res.Output)
		if res.Verdict != submission.StatusError {
			t.Fatalf("expected ERROR (timeout), got %s", res.Verdict)
		}
	})

	t.Run("output_limit", func(t *testing.T) {
		code := `import "fmt"

func Sum(nums []int) int { for i := 0; i < 1000000; i++ { fmt.Println("spam spam spam spam spam") }; return 6 }`
		res, err := r.Run(context.Background(), code, sumTest)
		t.Logf("verdict=%s err=%v outputLen=%d", res.Verdict, err, len(res.Output))
		if res.Verdict != submission.StatusError {
			t.Fatalf("expected ERROR (output limit), got %s", res.Verdict)
		}
		if len(res.Output) > maxOutputBytes+200 {
			t.Fatalf("output len = %d, want <= ~%d", len(res.Output), maxOutputBytes)
		}
	})

	t.Run("spoofed_pass_is_not_accepted", func(t *testing.T) {
		// writes a fake pass event straight to `go test`'s stdout, then kills it
		// so the real failing result is never reported
		code := `import (
	"os"
	"syscall"
)

func Sum(nums []int) int {
	if f, err := os.OpenFile("/proc/1/fd/1", os.O_WRONLY, 0); err == nil {
		f.WriteString("{\"Action\":\"pass\",\"Test\":\"TestSum\"}\n")
		f.Close()
	}
	syscall.Kill(1, syscall.SIGKILL)
	return 0
}`
		res, err := r.Run(context.Background(), code, sumTest)
		t.Logf("verdict=%s err=%v output=%s", res.Verdict, err, res.Output)
		if res.Verdict == submission.StatusPassed {
			t.Fatal("spoofed submission got PASSED")
		}
	})

	t.Run("missing_image_is_unavailable", func(t *testing.T) {
		res, err := Runner{image: "gojudge-does-not-exist:nope"}.Run(context.Background(), "func Sum(nums []int) int { return 6 }", sumTest)
		t.Logf("verdict=%s err=%v", res.Verdict, err)
		if !errors.Is(err, ErrSandboxUnavailable) {
			t.Fatalf("err = %v, want ErrSandboxUnavailable", err)
		}
	})

	t.Run("sandbox_runs_unprivileged", func(t *testing.T) {
		code := `import "os"

func Sum(nums []int) int { if os.Getuid() == 0 { panic("running as root") }; if os.WriteFile("/app/x", nil, 0644) == nil { panic("/app is writable") }; return 6 }`
		res, err := r.Run(context.Background(), code, sumTest)
		t.Logf("verdict=%s err=%v output=%s", res.Verdict, err, res.Output)
		if res.Verdict != submission.StatusPassed {
			t.Fatalf("expected PASSED, got %s: %s", res.Verdict, lastLines(res.Output))
		}
	})
}

func lastLines(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) > 5 {
		lines = lines[len(lines)-5:]
	}
	return strings.Join(lines, "\n")
}
