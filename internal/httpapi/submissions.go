package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sorenhoang/go-judge/internal/problem"
	"github.com/sorenhoang/go-judge/internal/queue"
	"github.com/sorenhoang/go-judge/internal/submission"
)

const streamPollInterval = 500 * time.Millisecond

type SubmissionHandler struct {
	submissionRepo submission.Repository
	problemRepo    problem.Repository
	publisher      *queue.Publisher
}

func NewSubmissionHandler(
	submissionRepo submission.Repository,
	problemRepo problem.Repository,
	publisher *queue.Publisher,
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

		if isTerminalStatus(s.Status) {
			return
		}

		select {
		case <-r.Context().Done():
			return
		case <-time.After(streamPollInterval):
		}
	}
}

func isTerminalStatus(s submission.Status) bool {
	switch s {
	case submission.StatusPassed, submission.StatusFailed, submission.StatusError:
		return true
	default:
		return false
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
