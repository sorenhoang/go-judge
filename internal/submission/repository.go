package submission

import (
	"context"
	"time"
)

type Repository interface {
	Create(ctx context.Context, s Submission) error
	GetByID(ctx context.Context, id string) (Submission, error)
	// Claim marks the submission RUNNING for this worker. It fails (false) if
	// it's finished or another worker claimed it less than lease ago, so two
	// workers never judge the same submission at once.
	Claim(ctx context.Context, id string, lease time.Duration) (bool, error)
	// UpdateResult records the final result unless the submission already has
	// one: the first result wins and a duplicate run can't overwrite it.
	UpdateResult(ctx context.Context, id string, status Status, output string, totalTests int, passedTests int) error
	// MarkForRetry counts a failed attempt and releases the claim, so the
	// retried job can claim it again without waiting for the lease.
	MarkForRetry(ctx context.Context, id string) error
	// RequeueStale claims up to limit unfinished submissions not touched for
	// staleAfter (and created within maxAge), bumps their updated_at and
	// returns them so they can be published again.
	RequeueStale(ctx context.Context, staleAfter, maxAge time.Duration, limit int) ([]Submission, error)
	// FailAbandoned marks unfinished submissions older than maxAge ERROR.
	FailAbandoned(ctx context.Context, maxAge time.Duration) (int64, error)
}
