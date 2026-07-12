package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/sorenhoang/go-judge/internal/problem"
)

type ProblemHandler struct {
	repo problem.Repository
}

func NewProblemHandler(repo problem.Repository) ProblemHandler {
	return ProblemHandler{repo: repo}
}

func (h ProblemHandler) RegisterRoutes(r chi.Router) {
	r.Get("/problems", h.listProblems)
	r.Get("/problems/{id}", h.getProblem)
}

func (h ProblemHandler) listProblems(w http.ResponseWriter, r *http.Request) {
	problems, err := h.repo.List(r.Context())
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, problems)
}

func (h ProblemHandler) getProblem(w http.ResponseWriter, r *http.Request) {
	p, err := h.repo.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		if errors.Is(err, problem.ErrNotFound) {
			http.Error(w, "problem not found", http.StatusNotFound)
			return
		}

		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, p)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}
