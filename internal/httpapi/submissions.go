package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sorenhoang/go-judge/internal/problem"
	"github.com/sorenhoang/go-judge/internal/queue"
	"github.com/sorenhoang/go-judge/internal/submission"
)

// vars so tests can shrink them
var (
	streamPollInterval = 500 * time.Millisecond
	// bounds a stream for a submission that never finishes; EventSource
	// reconnects on its own and gets the current status again
	streamMaxDuration = 2 * time.Minute
)

type publisher interface {
	Publish(ctx context.Context, msg queue.SubmissionCreated) error
}

type SubmissionHandler struct {
	submissionRepo submission.Repository
	problemRepo    problem.Repository
	publisher      publisher
}

func NewSubmissionHandler(
	submissionRepo submission.Repository,
	problemRepo problem.Repository,
	publisher publisher,
) SubmissionHandler {
	return SubmissionHandler{
		submissionRepo: submissionRepo,
		problemRepo:    problemRepo,
		publisher:      publisher,
	}
}

func (h SubmissionHandler) RegisterRoutes(r chi.Router) {
	r.Post("/submissions", h.createSubmission)
	r.Get("/submissions/{id}", h.getSubmission)
	r.Get("/submissions/{id}/stream", h.streamSubmission)
}

type createSubmissionRequest struct {
	ProblemID string `json:"problemId"`
	Code      string `json:"code"`
}

type createSubmissionResponse struct {
	SubmissionID string            `json:"submissionId"`
	Status       submission.Status `json:"status"`
}

func (h SubmissionHandler) createSubmission(w http.ResponseWriter, r *http.Request) {
	var req createSubmissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	if err := validateCreateSubmissionRequest(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if _, err := h.problemRepo.Get(r.Context(), req.ProblemID); err != nil {
		if errors.Is(err, problem.ErrNotFound) {
			http.Error(w, "problemId does not exist", http.StatusBadRequest)
			return
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	s := submission.Submission{
		ID:        uuid.NewString(),
		ProblemID: req.ProblemID,
		Code:      req.Code,
		Status:    submission.StatusPending,
	}

	if err := h.submissionRepo.Create(r.Context(), s); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := h.publisher.Publish(r.Context(), queue.SubmissionCreated{
		SubmissionID: s.ID,
		ProblemID:    s.ProblemID,
	}); err != nil {
		// nothing will ever pick this row up, so don't leave it PENDING forever
		ctx := context.WithoutCancel(r.Context())
		if updateErr := h.submissionRepo.UpdateResult(ctx, s.ID, submission.StatusError, "failed to queue submission: "+err.Error(), 0, 0); updateErr != nil {
			log.Printf("submission %s: publish failed (%v) and marking it ERROR failed: %v", s.ID, err, updateErr)
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, createSubmissionResponse{
		SubmissionID: s.ID,
		Status:       s.Status,
	})
}

func (h SubmissionHandler) getSubmission(w http.ResponseWriter, r *http.Request) {
	s, err := h.submissionRepo.GetByID(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		if errors.Is(err, submission.ErrNotFound) {
			http.Error(w, "submission not found", http.StatusNotFound)
			return
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, s)
}

func (h SubmissionHandler) streamSubmission(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if _, err := h.submissionRepo.GetByID(r.Context(), id); err != nil {
		if errors.Is(err, submission.ErrNotFound) {
			http.Error(w, "submission not found", http.StatusNotFound)
			return
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	send := func(s submission.Submission) error {
		payload, err := json.Marshal(s)
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte("data: " + string(payload) + "\n\n")); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	var lastStatus submission.Status
	deadline := time.After(streamMaxDuration)

	for {
		s, err := h.submissionRepo.GetByID(r.Context(), id)
		if err != nil {
			return
		}

		if s.Status != lastStatus {
			if err := send(s); err != nil {
				return
			}
			lastStatus = s.Status
		}

		if s.Status.IsTerminal() {
			return
		}

		select {
		case <-r.Context().Done():
			return
		case <-deadline:
			return
		case <-time.After(streamPollInterval):
		}
	}
}

func validateCreateSubmissionRequest(req createSubmissionRequest) error {
	if strings.TrimSpace(req.ProblemID) == "" {
		return errors.New("problemId is required")
	}
	if strings.TrimSpace(req.Code) == "" {
		return errors.New("code is required")
	}

	return nil
}
