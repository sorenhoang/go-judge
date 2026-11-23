package judge

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// a+b with two subtasks; test 0 is the sample.
var sumTests = []TestCase{
	{Input: "1 2\n", Answer: "3\n", Subtask: 0},
	{Input: "5 7\n", Answer: "12\n", Subtask: 1},
	{Input: "2000000000 2000000000\n", Answer: "4000000000\n", Subtask: 2}, // past INT_MAX, so `int` fails here
}

var sumSubtasks = []Subtask{{Index: 1, Points: 40}, {Index: 2, Points: 60}}

func requireSandboxImage(t *testing.T) {
	t.Helper()
	if err := exec.Command("docker", "image", "inspect", sandboxIOImage).Run(); err != nil {
		t.Fatalf("sandbox image %s missing, run `make sandbox-image`", sandboxIOImage)
	}
}

func sumRequest(lang Language, code string) IORequest {
	return IORequest{
		Language:      lang,
		Code:          code,
		Tests:         sumTests,
		Subtasks:      sumSubtasks,
		TimeLimit:     time.Second,
		MemoryLimitMB: 64,
	}
}

func runSum(t *testing.T, lang Language, code string) IOResult {
	t.Helper()
	res, err := NewRunner().RunIO(context.Background(), sumRequest(lang, code))
	if err != nil {
		t.Fatalf("RunIO error: %v", err)
	}
	t.Logf("verdict=%s score=%d tests=%+v compile=%q", res.Verdict, res.Score, res.Tests, res.CompileOutput)
	return res
}

func TestValidateIORequest(t *testing.T) {
	valid := sumRequest(LangCPP17, "int main(){}")

	cases := []struct {
		name   string
		mutate func(*IORequest)
	}{
		{"zero time limit", func(r *IORequest) { r.TimeLimit = 0 }},
		{"negative time limit", func(r *IORequest) { r.TimeLimit = -time.Second }},
		{"memory limit too small", func(r *IORequest) { r.MemoryLimitMB = 4 }},
		{"no tests", func(r *IORequest) { r.Tests = nil }},
		{"subtask without tests", func(r *IORequest) { r.Subtasks = append(r.Subtasks, Subtask{Index: 3, Points: 10}) }},
		{"test in undefined subtask", func(r *IORequest) { r.Tests = append(r.Tests, TestCase{Input: "1 1\n", Answer: "2\n", Subtask: 9}) }},
	}

	if err := validateIORequest(valid); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := valid
			req.Tests = append([]TestCase(nil), valid.Tests...)
			req.Subtasks = append([]Subtask(nil), valid.Subtasks...)
			tc.mutate(&req)
			if err := validateIORequest(req); !errors.Is(err, ErrInvalidIORequest) {
				t.Fatalf("err = %v, want ErrInvalidIORequest", err)
			}
		})
	}
}

func TestParseHarnessOutput(t *testing.T) {
	ok := "0 0 0.01 100\n1 0 0.02 200\n"
	runs, err := parseHarnessOutput([]byte(ok), 2)
	if err != nil || len(runs) != 2 || runs[1].MemoryKB != 200 {
		t.Fatalf("runs = %+v, err = %v", runs, err)
	}

	cases := map[string]string{
		"duplicate index (user code wrote to the harness's stdout)": "0 0 0.01 100\n1 0 0.02 200\n0 0 0.00 1\n",
		"index out of range": "0 0 0.01 100\n1 0 0.02 200\n7 0 0.00 1\n",
		"missing test":       "0 0 0.01 100\n",
	}
	for name, out := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseHarnessOutput([]byte(out), 2); !errors.Is(err, ErrHarnessTampered) {
				t.Fatalf("err = %v, want ErrHarnessTampered", err)
			}
		})
	}
}

func TestRunIO(t *testing.T) {
	if testing.Short() {
		t.Skip("sandbox test")
	}
	requireSandboxImage(t)

	t.Run("cpp AC", func(t *testing.T) {
		res := runSum(t, LangCPP17, `#include <bits/stdc++.h>
int main(){ long long a,b; std::cin>>a>>b; std::cout<<a+b<<"\n"; }`)
		if res.Verdict != VerdictAC || res.Score != 100 {
			t.Fatalf("want AC/100, got %s/%d", res.Verdict, res.Score)
		}
	})

	t.Run("cpp int overflow loses subtask 2", func(t *testing.T) {
		res := runSum(t, LangCPP17, `#include <bits/stdc++.h>
int main(){ int a,b; std::cin>>a>>b; std::cout<<a+b<<"\n"; }`)
		if res.Verdict != VerdictWA || res.Score != 40 {
			t.Fatalf("want WA/40, got %s/%d", res.Verdict, res.Score)
		}
	})

	t.Run("cpp CE", func(t *testing.T) {
		res := runSum(t, LangCPP17, `int main(){ this is not c++ }`)
		if res.Verdict != VerdictCE || res.CompileOutput == "" {
			t.Fatalf("want CE with compiler output, got %s %q", res.Verdict, res.CompileOutput)
		}
	})

	t.Run("cpp TLE", func(t *testing.T) {
		res := runSum(t, LangCPP17, `int main(){ volatile long long x=0; for(;;) x++; }`)
		if res.Verdict != VerdictTLE || res.Score != 0 {
			t.Fatalf("want TLE/0, got %s/%d", res.Verdict, res.Score)
		}
	})

	t.Run("cpp MLE", func(t *testing.T) {
		// touches 256MB against a 64MB limit
		res := runSum(t, LangCPP17, `#include <bits/stdc++.h>
int main(){ std::vector<char> v(256<<20, 1); std::cout<<(int)v[12345]<<"\n"; }`)
		if res.Verdict != VerdictMLE {
			t.Fatalf("want MLE, got %s", res.Verdict)
		}
	})

	t.Run("cpp RE", func(t *testing.T) {
		res := runSum(t, LangCPP17, `int main(){ int *p=nullptr; *p=1; }`)
		if res.Verdict != VerdictRE {
			t.Fatalf("want RE, got %s", res.Verdict)
		}
	})

	t.Run("python AC", func(t *testing.T) {
		res := runSum(t, LangPython3, "a, b = map(int, input().split())\nprint(a + b)\n")
		if res.Verdict != VerdictAC || res.Score != 100 {
			t.Fatalf("want AC/100, got %s/%d", res.Verdict, res.Score)
		}
	})

	t.Run("python syntax error is CE", func(t *testing.T) {
		res := runSum(t, LangPython3, "print(\n")
		if res.Verdict != VerdictCE {
			t.Fatalf("want CE, got %s", res.Verdict)
		}
	})

	t.Run("python exception is RE", func(t *testing.T) {
		res := runSum(t, LangPython3, "raise SystemExit(3)\n")
		if res.Verdict != VerdictRE {
			t.Fatalf("want RE, got %s", res.Verdict)
		}
	})

	t.Run("sandbox has no network", func(t *testing.T) {
		res := runSum(t, LangPython3, `import socket
socket.create_connection(("1.1.1.1", 53), timeout=0.5)
a, b = map(int, input().split())
print(a + b)
`)
		if res.Verdict != VerdictRE {
			t.Fatalf("want RE (network blocked), got %s", res.Verdict)
		}
	})

	t.Run("detached process can't finish work after the time limit", func(t *testing.T) {
		// exits at once, a detached child "computes" for longer than the limit and writes the answer later
		res := runSum(t, LangPython3, `import os, time
a, b = map(int, input().split())
if os.fork() == 0:
    os.setsid()
    time.sleep(1.5)
    open("/box/out/0.out", "w").write(str(a + b) + "\n")
    os._exit(0)
time.sleep(0.9)
`)
		if res.Tests[0].Verdict == VerdictAC {
			t.Fatalf("test 0 got AC from a background process: %+v", res.Tests)
		}
	})

	t.Run("spoofed harness line is rejected", func(t *testing.T) {
		_, err := NewRunner().RunIO(context.Background(), sumRequest(LangPython3, `a, b = map(int, input().split())
open("/proc/1/fd/1", "w").write("0 0 0.01 100\n")
print(a + b)
`))
		if !errors.Is(err, ErrHarnessTampered) {
			t.Fatalf("err = %v, want ErrHarnessTampered", err)
		}
	})

	t.Run("each test starts with an empty /tmp", func(t *testing.T) {
		res := runSum(t, LangPython3, `import os
seen = os.path.exists("/tmp/seen")
open("/tmp/seen", "w").write("x")
a, b = map(int, input().split())
print(-1 if seen else a + b)
`)
		if res.Verdict != VerdictAC {
			t.Fatalf("want AC, got %s (state leaked between tests)", res.Verdict)
		}
	})

	t.Run("compile output is capped", func(t *testing.T) {
		res := runSum(t, LangCPP17, strings.Repeat("undeclared_thing;\n", 100000))
		if res.Verdict != VerdictCE || len(res.CompileOutput) > maxOutputBytes+200 {
			t.Fatalf("want CE with <= ~%d bytes, got %s with %d bytes", maxOutputBytes, res.Verdict, len(res.CompileOutput))
		}
	})

	t.Run("sandbox has no capabilities", func(t *testing.T) {
		res := runSum(t, LangPython3, `caps = [l for l in open("/proc/self/status") if l.startswith("CapEff") or l.startswith("NoNewPrivs")]
if any(l.split()[1] not in ("0000000000000000", "1") for l in caps):
    raise SystemExit(1)
a, b = map(int, input().split())
print(a + b)
`)
		if res.Verdict != VerdictAC {
			t.Fatalf("want AC (no caps, no_new_privs), got %s", res.Verdict)
		}
	})

	t.Run("missing image is unavailable, not CE", func(t *testing.T) {
		r := NewRunner()
		r.ioImage = "gojudge-does-not-exist:nope"
		res, err := r.RunIO(context.Background(), sumRequest(LangCPP17, "int main(){}"))
		if !errors.Is(err, ErrSandboxUnavailable) {
			t.Fatalf("err = %v (verdict %s), want ErrSandboxUnavailable", err, res.Verdict)
		}
	})
}
