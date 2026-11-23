package main

import (
	"context"
	"net"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sorenhoang/go-judge/internal/judge"
	"github.com/sorenhoang/go-judge/internal/runnerclient"
)

// slowCleanupRunner blocks until its run is cancelled, then takes a while to
// clean up, like judge.Runner removing its container.
type slowCleanupRunner struct {
	started   chan struct{}
	cleanedUp atomic.Bool
}

func (r *slowCleanupRunner) Run(ctx context.Context, _, _ string) (judge.Result, error) {
	close(r.started)
	<-ctx.Done()
	time.Sleep(500 * time.Millisecond)
	r.cleanedUp.Store(true)
	return judge.Result{}, ctx.Err()
}

func TestServeWaitsForCancelledRunsToCleanUp(t *testing.T) {
	old := shutdownGrace
	shutdownGrace = 100 * time.Millisecond
	defer func() { shutdownGrace = old }()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r := &slowCleanupRunner{started: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- serve(ctx, lis, r) }()

	client, closeClient, err := runnerclient.Dial(lis.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer closeClient()
	go client.Run(context.Background(), "", "")

	<-r.started
	cancel()
	if err := <-served; err != nil {
		t.Fatalf("serve: %v", err)
	}
	// main exits as soon as serve returns, so cleanup must already be done
	if !r.cleanedUp.Load() {
		t.Fatal("serve returned before the cancelled run finished cleaning up")
	}
}

func TestShutdownLeavesNoSandboxContainers(t *testing.T) {
	if testing.Short() {
		t.Skip("needs a Docker daemon and the gojudge-gotest image")
	}

	old := shutdownGrace
	shutdownGrace = time.Second
	defer func() { shutdownGrace = old }()

	// the runner bind-mounts a dir under TMPDIR, which identifies our container
	// among other packages' sandbox tests running in parallel
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- serve(ctx, lis, judge.NewRunner()) }()

	client, closeClient, err := runnerclient.Dial(lis.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer closeClient()

	go client.Run(context.Background(), "func Sum(n []int) int { for {} }", "func TestSum(t *testing.T) { Sum(nil) }")

	waitFor(t, func() bool { return len(ourContainers(t, tmp)) > 0 }, "sandbox container to start")
	cancel()

	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("serve didn't return after shutdown")
	}

	if left := ourContainers(t, tmp); len(left) > 0 {
		t.Fatalf("containers left running after shutdown: %v", left)
	}
}

// ourContainers returns running sandbox containers that bind-mount a dir under tmp.
func ourContainers(t *testing.T, tmp string) []string {
	t.Helper()
	out, err := exec.Command("docker", "ps", "-q", "--filter", "name=gojudge-").Output()
	if err != nil {
		t.Fatal(err)
	}
	var ours []string
	for _, id := range strings.Fields(string(out)) {
		// errors only if the container exited meanwhile, which means it isn't running
		mounts, _ := exec.Command("docker", "inspect", "-f", "{{range .Mounts}}{{.Source}} {{end}}", id).Output()
		if strings.Contains(string(mounts), tmp) {
			ours = append(ours, id)
		}
	}
	return ours
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}
