// Package sweeper recovers submissions whose job was lost: the API died
// between the insert and the publish, the broker dropped the message, or the
// queue's delivery limit dead-lettered it without a final status. Republishing
// is safe because the worker acks submissions that are already finished.
package sweeper

import (
	"context"
	"log"
	"time"

	"github.com/sorenhoang/go-judge/internal/queue"
	"github.com/sorenhoang/go-judge/internal/submission"
)

const (
	Interval = time.Minute
	// StaleAfter must be well above the worker's longest job (45s timeout
	// plus backoff), or a submission still being judged would be republished.
	StaleAfter = 5 * time.Minute
	// MaxAge bounds the recovery: past it the submission is marked ERROR
	// instead, so a job that can never succeed isn't republished forever.
	MaxAge    = time.Hour
	BatchSize = 100
)

type store interface {
	RequeueStale(ctx context.Context, staleAfter, maxAge time.Duration, limit int) ([]submission.Submission, error)
	FailAbandoned(ctx context.Context, maxAge time.Duration) (int64, error)
}

type publisher interface {
	Publish(ctx context.Context, msg queue.SubmissionCreated) error
}

func Run(ctx context.Context, s store, p publisher) {
	ticker := time.NewTicker(Interval)
	defer ticker.Stop()
	for {
		SweepOnce(ctx, s, p)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func SweepOnce(ctx context.Context, s store, p publisher) {
	stale, err := s.RequeueStale(ctx, StaleAfter, MaxAge, BatchSize)
	if err != nil {
		log.Printf("sweeper: finding stale submissions failed: %v", err)
	}
	for _, sub := range stale {
		// a failed publish is picked up again once StaleAfter passes
		if err := p.Publish(ctx, queue.SubmissionCreated{SubmissionID: sub.ID, ProblemID: sub.ProblemID}); err != nil {
			log.Printf("sweeper: republishing submission %s failed: %v", sub.ID, err)
			continue
		}
		log.Printf("sweeper: republished stale submission %s", sub.ID)
	}

	failed, err := s.FailAbandoned(ctx, MaxAge)
	if err != nil {
		log.Printf("sweeper: failing abandoned submissions failed: %v", err)
	}
	if failed > 0 {
		log.Printf("sweeper: marked %d abandoned submissions ERROR", failed)
	}
}
