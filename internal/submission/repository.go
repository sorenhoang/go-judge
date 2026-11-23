package submission

import (
	"context"
	"time"
)

type Repository interface {
	Create(ctx context.Context, s Submission) error
	GetByID(ctx context.Context, id string) (Submission, error)
	UpdateStatus(ctx context.Context, id string, status Status) error
	UpdateResult(ctx context.Context, id string, status Status, output string, totalTests int, passedTests int) error
	IncrementRetryCount(ctx context.Context, id string) error
	// RequeueStale claims up to limit unfinished submissions not touched for
	// staleAfter (and created within maxAge), bumps their updated_at and
	// returns them so they can be published again.
	RequeueStale(ctx context.Context, staleAfter, maxAge time.Duration, limit int) ([]Submission, error)
	// FailAbandoned marks unfinished submissions older than maxAge ERROR.
	FailAbandoned(ctx context.Context, maxAge time.Duration) (int64, error)
}
