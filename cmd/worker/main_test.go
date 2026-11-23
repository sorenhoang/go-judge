package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sorenhoang/go-judge/internal/problem"
	"github.com/sorenhoang/go-judge/internal/queue"
	"github.com/sorenhoang/go-judge/internal/runnerclient"
	"github.com/sorenhoang/go-judge/internal/submission"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestPlanAlreadyFinishedSubmissionAcksWithoutProcessing(t *testing.T) {
	result := planJobResult(submission.Submission{
		Status: submission.StatusPassed,
	}, nil, maxRetries)

	if result != queue.Ack {
		t.Fatalf("result = %v, want %v", result, queue.Ack)
	}
}

// RetryCount is the number of failed attempts so far, so maxRetries retries
// means up to maxRetries+1 attempts: the original one plus maxRetries retries.
func TestPlanRetryableFailureRetriesUpToMaxRetries(t *testing.T) {
	for failed := 1; failed <= maxRetries; failed++ {
		result := planJobResult(submission.Submission{
			Status:     submission.StatusRunning,
			RetryCount: failed,
		}, context.DeadlineExceeded, maxRetries)

		if result != queue.Retry {
			t.Fatalf("after %d failed attempts: result = %v, want %v (retry %d/%d)", failed, result, queue.Retry, failed, maxRetries)
		}
	}
}

func TestPlanRetryableFailureDeadLettersAfterMaxRetries(t *testing.T) {
	result := planJobResult(submission.Submission{
		Status:     submission.StatusRunning,
		RetryCount: maxRetries + 1,
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

func TestUnavailableRunnerIsRetryable(t *testing.T) {
	if !isRetryableRunnerError(status.Error(codes.Unavailable, "sandbox unavailable")) {
		t.Fatal("codes.Unavailable should be retryable")
	}
}

type fakeSubmissions struct {
	submission.Repository
	sub         submission.Submission
	getErr      error
	heldByOther bool // another worker holds the claim
	resultSet   submission.Status
}

func (f *fakeSubmissions) GetByID(context.Context, string) (submission.Submission, error) {
	return f.sub, f.getErr
}

func (f *fakeSubmissions) Claim(context.Context, string, time.Duration) (bool, error) {
	if f.heldByOther {
		return false, nil
	}
	f.sub.Status = submission.StatusRunning
	return true, nil
}

func (f *fakeSubmissions) UpdateResult(_ context.Context, _ string, s submission.Status, _ string, _ int, _ int) error {
	f.resultSet = s
	return nil
}

func (f *fakeSubmissions) MarkForRetry(context.Context, string) error { return nil }

type fakeProblems struct {
	problem.Repository
	err error
}

func (f fakeProblems) Get(context.Context, string) (problem.Problem, error) {
	return problem.Problem{TestCode: "func TestX(t *testing.T) {}"}, f.err
}

type fakeRunner struct{ calls *int }

func (r fakeRunner) Run(context.Context, string, string) (runnerclient.Result, error) {
	if r.calls != nil {
		*r.calls++
	}
	return runnerclient.Result{Verdict: submission.StatusPassed, TotalTests: 1, PassedTests: 1}, nil
}

func TestHandleJobDoesNotRunASubmissionAnotherWorkerHolds(t *testing.T) {
	subs := &fakeSubmissions{sub: submission.Submission{Status: submission.StatusRunning}, heldByOther: true}
	calls := 0

	got := handleJob(subs, fakeProblems{}, fakeRunner{calls: &calls})(context.Background(), queue.SubmissionCreated{SubmissionID: "s1"})

	// retried later: by then the other worker has finished (ack) or died (lease expired)
	if got != queue.Retry {
		t.Fatalf("result = %v, want %v", got, queue.Retry)
	}
	if calls != 0 {
		t.Fatalf("sandbox ran %d times for a submission another worker holds", calls)
	}
}

func TestHandleJob(t *testing.T) {
	transient := errors.New("connection refused")

	cases := []struct {
		name       string
		getErr     error
		problemErr error
		want       queue.HandlerResult
		wantStatus submission.Status // terminal status written, "" if none
	}{
		{"happy path", nil, nil, queue.Ack, submission.StatusPassed},
		{"submission lookup transient error retries", transient, nil, queue.Retry, ""},
		{"submission missing dead-letters", submission.ErrNotFound, nil, queue.DeadLetter, ""},
		{"problem lookup transient error retries", nil, transient, queue.Retry, ""},
		{"problem missing marks ERROR before dead-lettering", nil, problem.ErrNotFound, queue.DeadLetter, submission.StatusError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			subs := &fakeSubmissions{sub: submission.Submission{Status: submission.StatusPending}, getErr: tc.getErr}
			handler := handleJob(subs, fakeProblems{err: tc.problemErr}, fakeRunner{})

			got := handler(context.Background(), queue.SubmissionCreated{SubmissionID: "s1", ProblemID: "p1"})

			if got != tc.want {
				t.Fatalf("result = %v, want %v", got, tc.want)
			}
			if subs.resultSet != tc.wantStatus {
				t.Fatalf("terminal status = %q, want %q", subs.resultSet, tc.wantStatus)
			}
		})
	}
}
