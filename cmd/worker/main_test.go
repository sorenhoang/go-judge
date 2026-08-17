package main

import (
	"context"
	"errors"
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
