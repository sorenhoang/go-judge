package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	_ "github.com/jackc/pgx/v5/stdlib"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/sorenhoang/go-judge/internal/config"
	"github.com/sorenhoang/go-judge/internal/httpapi"
	"github.com/sorenhoang/go-judge/internal/problem"
	"github.com/sorenhoang/go-judge/internal/queue"
	"github.com/sorenhoang/go-judge/internal/submission"
	"github.com/sorenhoang/go-judge/internal/sweeper"
)

// shutdownGrace lets in-flight requests (e.g. a create between its insert and
// publish) finish. Open SSE streams are then closed; EventSource reconnects.
const shutdownGrace = 5 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// once shutdown starts, a second Ctrl-C/SIGTERM kills the process right away
	context.AfterFunc(ctx, stop)

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

	amqpConn, err := amqp.Dial(cfg.RabbitMQURL)
	if err != nil {
		log.Fatal(err)
	}
	defer amqpConn.Close()

	publisher, err := queue.NewPublisher(amqpConn)
	if err != nil {
		log.Fatal(err)
	}

	problemHandler := httpapi.NewProblemHandler(problemRepo)
	submissionHandler := httpapi.NewSubmissionHandler(submissionRepo, problemRepo, publisher)

	problemHandler.RegisterRoutes(r)
	submissionHandler.RegisterRoutes(r)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	go sweeper.Run(ctx, submissionRepo, publisher)

	srv := &http.Server{Addr: ":8080", Handler: r}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		log.Printf("shutting down, waiting up to %s for in-flight requests", shutdownGrace)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			srv.Close() // only long-lived SSE streams are left by now
		}
	}()

	log.Println("Starting server on :8080")
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	<-shutdownDone
	log.Println("api shutdown complete")
}
