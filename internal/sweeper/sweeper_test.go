package sweeper

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sorenhoang/go-judge/internal/queue"
	"github.com/sorenhoang/go-judge/internal/submission"
)

type fakeStore struct {
	stale        []submission.Submission
	failedMaxAge time.Duration
}

func (f *fakeStore) RequeueStale(context.Context, time.Duration, time.Duration, int) ([]submission.Submission, error) {
	return f.stale, nil
}

func (f *fakeStore) FailAbandoned(_ context.Context, maxAge time.Duration) (int64, error) {
	f.failedMaxAge = maxAge
	return 0, nil
}

type recordingPublisher struct {
	published []queue.SubmissionCreated
	failOn    string
}

func (p *recordingPublisher) Publish(_ context.Context, msg queue.SubmissionCreated) error {
	if msg.SubmissionID == p.failOn {
		return errors.New("channel closed")
	}
	p.published = append(p.published, msg)
	return nil
}

func TestSweepOnceRepublishesStaleAndFailsAbandoned(t *testing.T) {
	store := &fakeStore{stale: []submission.Submission{
		{ID: "a", ProblemID: "p1"},
		{ID: "b", ProblemID: "p2"},
		{ID: "c", ProblemID: "p3"},
	}}
	pub := &recordingPublisher{failOn: "b"}

	SweepOnce(context.Background(), store, pub)

	// one failed publish doesn't stop the rest; "b" is retried on a later sweep
	want := []queue.SubmissionCreated{{SubmissionID: "a", ProblemID: "p1"}, {SubmissionID: "c", ProblemID: "p3"}}
	if len(pub.published) != len(want) || pub.published[0] != want[0] || pub.published[1] != want[1] {
		t.Fatalf("published = %+v, want %+v", pub.published, want)
	}
	if store.failedMaxAge != MaxAge {
		t.Fatalf("FailAbandoned maxAge = %s, want %s", store.failedMaxAge, MaxAge)
	}
}
