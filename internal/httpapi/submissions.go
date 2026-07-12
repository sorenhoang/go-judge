package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sorenhoang/go-judge/internal/judge"
	"github.com/sorenhoang/go-judge/internal/problem"
	"github.com/sorenhoang/go-judge/internal/submission"
)

type SubmissionHandler struct {
	submissionRepo submission.Repository
	problemRepo    problem.Repository
	judgeRunner    judge.Runner
}

func NewSubmissionHandler(submissionRepo submission.Repository, problemRepo problem.Repository, judgeRunner judge.Runner) SubmissionHandler {
	return SubmissionHandler{submissionRepo: submissionRepo, problemRepo: problemRepo, judgeRunner: judgeRunner}
}

func (h SubmissionHandler) RegisterRoutes(r chi.Router) {
	r.Post("/submissions", h.createSubmission)
	r.Get("/submissions/{id}", h.getSubmission)
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

	prob, err := h.problemRepo.Get(r.Context(), req.ProblemID)
	if err != nil {
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

	result, err := h.judgeRunner.Run(r.Context(), req.Code, prob.TestCode)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := h.submissionRepo.UpdateResult(
		r.Context(),
		s.ID,
		result.Verdict,
		result.Output,
		result.TotalTests,
		result.PassedTests,
	); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, createSubmissionResponse{
		SubmissionID: s.ID,
		Status:       result.Verdict,
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

func validateCreateSubmissionRequest(req createSubmissionRequest) error {
	if strings.TrimSpace(req.ProblemID) == "" {
		return errors.New("problemId is required")
	}
	if strings.TrimSpace(req.Code) == "" {
		return errors.New("code is required")
	}

	return nil
}
