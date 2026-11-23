package judge

import (
	"testing"
	"time"
)

func TestTokensEqual(t *testing.T) {
	cases := []struct {
		name   string
		output string
		answer string
		want   bool
	}{
		{"identical", "1 2 3\n", "1 2 3\n", true},
		{"trailing whitespace and newline ignored", "1 2 3   \n\n", "1 2 3", true},
		{"line breaks vs spaces ignored", "1\n2\n3", "1 2 3", true},
		{"windows line endings", "1 2\r\n3\r\n", "1 2\n3\n", true},
		{"different token", "1 2 4", "1 2 3", false},
		{"missing token", "1 2", "1 2 3", false},
		{"extra token", "1 2 3 4", "1 2 3", false},
		{"empty vs empty", "", "\n", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := tokensEqual(c.output, c.answer); got != c.want {
				t.Fatalf("tokensEqual(%q, %q) = %v, want %v", c.output, c.answer, got, c.want)
			}
		})
	}
}

func TestClassifyRun(t *testing.T) {
	limit := time.Second
	cases := []struct {
		name string
		run  harnessRun
		want Verdict
	}{
		{"clean exit", harnessRun{ExitCode: 0, Elapsed: 100 * time.Millisecond}, VerdictAC},
		{"killed by timeout", harnessRun{ExitCode: 137, Elapsed: limit}, VerdictTLE},
		{"slow but exited", harnessRun{ExitCode: 0, Elapsed: limit + time.Millisecond}, VerdictTLE},
		{"killed by OOM before time limit", harnessRun{ExitCode: 137, Elapsed: 200 * time.Millisecond}, VerdictMLE},
		{"non-zero exit", harnessRun{ExitCode: 1, Elapsed: 10 * time.Millisecond}, VerdictRE},
		{"segfault", harnessRun{ExitCode: 139, Elapsed: 10 * time.Millisecond}, VerdictRE},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// AC here only means "ran cleanly"; the output check happens after.
			if got := classifyRun(c.run, limit); got != c.want {
				t.Fatalf("classifyRun(%+v) = %s, want %s", c.run, got, c.want)
			}
		})
	}
}

func TestParseHarnessLine(t *testing.T) {
	got, err := parseHarnessLine("3 137 1.02 65536")
	if err != nil {
		t.Fatal(err)
	}
	want := harnessRun{Index: 3, ExitCode: 137, Elapsed: 1020 * time.Millisecond, MemoryKB: 65536}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	if _, err := parseHarnessLine("garbage"); err == nil {
		t.Fatal("expected error for malformed line")
	}
}

func TestScore(t *testing.T) {
	subtasks := []Subtask{{Index: 1, Points: 30}, {Index: 2, Points: 70}}
	tests := []TestCase{
		{Subtask: 0}, // sample: never scored
		{Subtask: 1},
		{Subtask: 1},
		{Subtask: 2},
		{Subtask: 2},
	}

	cases := []struct {
		name     string
		verdicts []Verdict
		want     int
	}{
		{"all AC", []Verdict{VerdictAC, VerdictAC, VerdictAC, VerdictAC, VerdictAC}, 100},
		{"sample fails, subtasks pass", []Verdict{VerdictWA, VerdictAC, VerdictAC, VerdictAC, VerdictAC}, 100},
		{"one test in subtask 2 fails", []Verdict{VerdictAC, VerdictAC, VerdictAC, VerdictAC, VerdictTLE}, 30},
		{"one test in subtask 1 fails", []Verdict{VerdictAC, VerdictWA, VerdictAC, VerdictAC, VerdictAC}, 70},
		{"nothing passes", []Verdict{VerdictWA, VerdictWA, VerdictWA, VerdictWA, VerdictWA}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			results := make([]TestResult, len(c.verdicts))
			for i, v := range c.verdicts {
				results[i] = TestResult{Verdict: v}
			}
			if got := score(tests, results, subtasks); got != c.want {
				t.Fatalf("score = %d, want %d", got, c.want)
			}
		})
	}
}

func TestOverallVerdict(t *testing.T) {
	results := []TestResult{{Verdict: VerdictAC}, {Verdict: VerdictTLE}, {Verdict: VerdictWA}}
	if got := overallVerdict(results); got != VerdictTLE {
		t.Fatalf("got %s, want first non-AC verdict TLE", got)
	}
	if got := overallVerdict([]TestResult{{Verdict: VerdictAC}}); got != VerdictAC {
		t.Fatalf("got %s, want AC", got)
	}
}
