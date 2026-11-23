package judge

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

type Verdict string

const (
	VerdictAC  Verdict = "AC"
	VerdictWA  Verdict = "WA"
	VerdictTLE Verdict = "TLE"
	VerdictMLE Verdict = "MLE"
	VerdictRE  Verdict = "RE"
	VerdictCE  Verdict = "CE"
)

// SIGKILL: sent by `timeout` at the time limit, or by the cgroup OOM killer.
const exitKilled = 137

type TestCase struct {
	Input   string
	Answer  string
	Subtask int // 0 = sample, never scored
}

type Subtask struct {
	Index  int
	Points int
}

type TestResult struct {
	Verdict  Verdict `json:"verdict"`
	TimeMs   int     `json:"time_ms"`
	MemoryKB int     `json:"memory_kb"`
}

type harnessRun struct {
	Index    int
	ExitCode int
	Elapsed  time.Duration
	MemoryKB int
}

func tokensEqual(output, answer string) bool {
	return slices.Equal(strings.Fields(output), strings.Fields(answer))
}

// classifyRun returns AC when the process ran cleanly; the output check is separate.
func classifyRun(run harnessRun, limit time.Duration) Verdict {
	switch {
	case run.Elapsed >= limit:
		return VerdictTLE
	case run.ExitCode == exitKilled:
		return VerdictMLE
	case run.ExitCode != 0:
		return VerdictRE
	default:
		return VerdictAC
	}
}

// parseHarnessLine parses "N exit_code elapsed_seconds max_rss_kb" from sandbox/harness.sh.
func parseHarnessLine(line string) (harnessRun, error) {
	f := strings.Fields(line)
	if len(f) != 4 {
		return harnessRun{}, fmt.Errorf("malformed harness line %q", line)
	}
	idx, err1 := strconv.Atoi(f[0])
	code, err2 := strconv.Atoi(f[1])
	secs, err3 := strconv.ParseFloat(f[2], 64)
	mem, err4 := strconv.Atoi(f[3])
	for _, err := range []error{err1, err2, err3, err4} {
		if err != nil {
			return harnessRun{}, fmt.Errorf("malformed harness line %q: %w", line, err)
		}
	}
	return harnessRun{
		Index:    idx,
		ExitCode: code,
		Elapsed:  time.Duration(secs * float64(time.Second)).Round(time.Millisecond),
		MemoryKB: mem,
	}, nil
}

// score gives a subtask its points only if every test in it is AC.
func score(tests []TestCase, results []TestResult, subtasks []Subtask) int {
	failed := map[int]bool{}
	for i, tc := range tests {
		if results[i].Verdict != VerdictAC {
			failed[tc.Subtask] = true
		}
	}
	total := 0
	for _, st := range subtasks {
		if !failed[st.Index] {
			total += st.Points
		}
	}
	return total
}

func overallVerdict(results []TestResult) Verdict {
	for _, r := range results {
		if r.Verdict != VerdictAC {
			return r.Verdict
		}
	}
	return VerdictAC
}
