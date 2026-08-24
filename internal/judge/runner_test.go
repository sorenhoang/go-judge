package judge

import (
	"context"
	"testing"
	"time"
)

func TestSmokeSandbox(t *testing.T) {
	r := NewRunner()

	t.Run("passing", func(t *testing.T) {
		code := `func Sum(nums []int) int { total := 0; for _, n := range nums { total += n }; return total }`
		testCode := `func TestSum(t *testing.T) { if Sum([]int{1,2,3}) != 6 { t.Fatal("wrong") } }`
		res, err := r.Run(context.Background(), code, testCode)
		t.Logf("verdict=%s passed=%d total=%d err=%v output=%s", res.Verdict, res.PassedTests, res.TotalTests, err, res.Output)
		if res.Verdict != "PASSED" {
			t.Fatalf("expected PASSED, got %s", res.Verdict)
		}
	})

	t.Run("failing", func(t *testing.T) {
		code := `func Sum(nums []int) int { return 0 }`
		testCode := `func TestSum(t *testing.T) { if Sum([]int{1,2,3}) != 6 { t.Fatal("wrong") } }`
		res, err := r.Run(context.Background(), code, testCode)
		t.Logf("verdict=%s passed=%d total=%d err=%v output=%s", res.Verdict, res.PassedTests, res.TotalTests, err, res.Output)
		if res.Verdict != "FAILED" {
			t.Fatalf("expected FAILED, got %s", res.Verdict)
		}
	})

	t.Run("timeout_and_cleanup", func(t *testing.T) {
		code := `func Sum(nums []int) int { for {} }`
		testCode := `func TestSum(t *testing.T) { Sum([]int{1}) }`
		start := time.Now()
		res, err := r.Run(context.Background(), code, testCode)
		t.Logf("elapsed=%s verdict=%s err=%v output=%s", time.Since(start), res.Verdict, err, res.Output)
		if res.Verdict != "ERROR" {
			t.Fatalf("expected ERROR (timeout), got %s", res.Verdict)
		}
	})

	t.Run("sandbox_blocks_host_file_read", func(t *testing.T) {
		code := `import "os"

func Sum(nums []int) int { b, err := os.ReadFile("/etc/hostname"); if err != nil { panic(err) }; return len(b) }`
		testCode := `func TestSum(t *testing.T) { Sum([]int{1}) }`
		res, err := r.Run(context.Background(), code, testCode)
		t.Logf("verdict=%s err=%v output=%s", res.Verdict, err, res.Output)
	})
}
