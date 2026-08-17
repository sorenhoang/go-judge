package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sorenhoang/go-judge/internal/queue"
	"github.com/sorenhoang/go-judge/internal/submission"
)

func TestPlanAlreadyFinishedSubmissionAcksWithoutProcessing(t *testing.T) {
	result := planJobResult(submission.Submission{
		Status: submission.StatusPassed,
	}, nil, maxRetries)

	if result != queue.Ack {
		t.Fatalf("result = %v, want %v", result, queue.Ack)
	}
}

func TestPlanRetryableFailureRetriesBeforeMaxRetries(t *testing.T) {
	result := planJobResult(submission.Submission{
		Status:     submission.StatusRunning,
		RetryCount: 1,
	}, context.DeadlineExceeded, maxRetries)

	if result != queue.Retry {
		t.Fatalf("result = %v, want %v", result, queue.Retry)
	}
}

func TestPlanRetryableFailureDeadLettersAtMaxRetries(t *testing.T) {
	result := planJobResult(submission.Submission{
		Status:     submission.StatusRunning,
		RetryCount: maxRetries,
	}, context.DeadlineExceeded, maxRetries)

	if result != queue.DeadLetter {
		t.Fatalf("result = %v, want %v", result, queue.DeadLetter)
	}
}

func TestPlanPermanentFailureDeadLetters(t *testing.T) {
	result := planJobResult(submission.Submission{
		Status:     submission.StatusRunning,
		RetryCount: 0,
	}, errors.New("problem not found"), maxRetries)

	if result != queue.DeadLetter {
		t.Fatalf("result = %v, want %v", result, queue.DeadLetter)
	}
}

func TestTrackJobForwardsConsumerContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var wg sync.WaitGroup
	var inFlight int64
	called := false

	wrapped := trackJob(&wg, &inFlight, func(ctx context.Context, _ queue.SubmissionCreated) queue.HandlerResult {
		called = true
		if ctx.Err() != context.Canceled {
			t.Fatalf("ctx.Err() = %v, want %v", ctx.Err(), context.Canceled)
		}
		return queue.Ack
	})

	result := wrapped(ctx, queue.SubmissionCreated{})
	wg.Wait()

	if result != queue.Ack {
		t.Fatalf("result = %v, want %v", result, queue.Ack)
	}
	if !called {
		t.Fatal("handler was not called")
	}
	if got := atomic.LoadInt64(&inFlight); got != 0 {
		t.Fatalf("inFlight = %d, want 0", got)
	}
}
