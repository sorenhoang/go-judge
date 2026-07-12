package main

import (
	"database/sql"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/sorenhoang/go-judge/internal/config"
	"github.com/sorenhoang/go-judge/internal/httpapi"
	"github.com/sorenhoang/go-judge/internal/judge"
	"github.com/sorenhoang/go-judge/internal/problem"
	"github.com/sorenhoang/go-judge/internal/submission"
)

func main() {
	r := chi.NewRouter()

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"http://localhost:5173"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type"},
	}))

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}

	problemRepo := problem.NewPostgresRepository(db)
	submissionRepo := submission.NewPostgresRepository(db)
	judgeRunner := judge.NewRunner()

	problemHandler := httpapi.NewProblemHandler(problemRepo)
	submissionHandler := httpapi.NewSubmissionHandler(submissionRepo, problemRepo, judgeRunner)

	problemHandler.RegisterRoutes(r)
	submissionHandler.RegisterRoutes(r)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	log.Println("Starting server on :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}
