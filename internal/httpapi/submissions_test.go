package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sorenhoang/go-judge/internal/problem"
	"github.com/sorenhoang/go-judge/internal/queue"
	"github.com/sorenhoang/go-judge/internal/submission"
)

type fakeSubmissions struct {
	submission.Repository
	status    submission.Status
	resultSet submission.Status
}

func (f *fakeSubmissions) Create(context.Context, submission.Submission) error { return nil }

func (f *fakeSubmissions) GetByID(_ context.Context, id string) (submission.Submission, error) {
	return submission.Submission{ID: id, Status: f.status}, nil
}

func (f *fakeSubmissions) UpdateResult(_ context.Context, _ string, s submission.Status, _ string, _ int, _ int) error {
	f.resultSet = s
	return nil
}

type fakeProblems struct{ problem.Repository }

func (fakeProblems) Get(context.Context, string) (problem.Problem, error) {
	return problem.Problem{}, nil
}

type failingPublisher struct{}

func (failingPublisher) Publish(context.Context, queue.SubmissionCreated) error {
	return errors.New("channel closed")
}

func newRouter(subs *fakeSubmissions, pub publisher) http.Handler {
	r := chi.NewRouter()
	NewSubmissionHandler(subs, fakeProblems{}, pub).RegisterRoutes(r)
	return r
}

func TestCreateSubmissionPublishFailureMarksError(t *testing.T) {
	subs := &fakeSubmissions{}
	req := httptest.NewRequest(http.MethodPost, "/submissions", strings.NewReader(`{"problemId":"sum-slice","code":"func Sum() {}"}`))
	rec := httptest.NewRecorder()

	newRouter(subs, failingPublisher{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if subs.resultSet != submission.StatusError {
		t.Fatalf("submission status = %q, want ERROR so it isn't left PENDING forever", subs.resultSet)
	}
}

func TestStreamSubmissionStopsAfterMaxDuration(t *testing.T) {
	oldMax, oldPoll := streamMaxDuration, streamPollInterval
	streamMaxDuration, streamPollInterval = 50*time.Millisecond, 5*time.Millisecond
	defer func() { streamMaxDuration, streamPollInterval = oldMax, oldPoll }()

	subs := &fakeSubmissions{status: submission.StatusRunning}
	req := httptest.NewRequest(http.MethodGet, "/submissions/s1/stream", nil)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		newRouter(subs, failingPublisher{}).ServeHTTP(rec, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream never closed for a submission stuck in RUNNING")
	}
	if !strings.Contains(rec.Body.String(), `"status":"RUNNING"`) {
		t.Fatalf("body = %q, want a RUNNING event", rec.Body.String())
	}
}
