package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/sorenhoang/go-judge/internal/config"
	"github.com/sorenhoang/go-judge/internal/problem"
	"github.com/sorenhoang/go-judge/internal/queue"
	"github.com/sorenhoang/go-judge/internal/runnerclient"
	"github.com/sorenhoang/go-judge/internal/submission"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const maxRetries = 3
const submissionTimeout = 15 * time.Second
const shutdownTimeout = 20 * time.Second

type runner interface {
	Run(ctx context.Context, code string, testCode string) (runnerclient.Result, error)
}

func isTerminalStatus(status submission.Status) bool {
	return status == submission.StatusPassed || status == submission.StatusFailed || status == submission.StatusError
}

func isRetryableRunnerError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}

	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
		return true
	default:
		return false
	}
}

func planJobResult(sub submission.Submission, err error, maxRetries int) queue.HandlerResult {
	if err == nil {
		return queue.Ack
	}

	if isRetryableRunnerError(err) && sub.RetryCount < maxRetries {
		return queue.Retry
	}

	return queue.DeadLetter
}

func handleJob(subRepo submission.Repository, probRepo problem.Repository, runner runner) func(context.Context, queue.SubmissionCreated) queue.HandlerResult {
	return func(ctx context.Context, msg queue.SubmissionCreated) queue.HandlerResult {
		log.Printf("received job for submission %s", msg.SubmissionID)

		sub, err := subRepo.GetByID(ctx, msg.SubmissionID)
		if err != nil {
			log.Printf("submission %s lookup failed: %v", msg.SubmissionID, err)
			return queue.DeadLetter
		}

		if isTerminalStatus(sub.Status) {
			log.Printf("submission %s already finished with status %s, acking", msg.SubmissionID, sub.Status)
			return queue.Ack
		}

		if err := subRepo.UpdateStatus(ctx, msg.SubmissionID, submission.StatusRunning); err != nil {
			log.Printf("submission %s status update failed: %v", msg.SubmissionID, err)
			return queue.Retry
		}

		prob, err := probRepo.Get(ctx, msg.ProblemID)
		if err != nil {
			log.Printf("problem %s lookup failed for submission %s: %v", msg.ProblemID, msg.SubmissionID, err)
			return queue.DeadLetter
		}

		jobCtx, cancel := context.WithTimeout(ctx, submissionTimeout)
		defer cancel()

		result, err := runner.Run(jobCtx, sub.Code, prob.TestCode)
		if err != nil {
			if !isRetryableRunnerError(err) {
				output := "runner failed permanently: " + err.Error()
				if updateErr := subRepo.UpdateResult(context.Background(), msg.SubmissionID, submission.StatusError, output, 0, 0); updateErr != nil {
					log.Printf("submission %s permanent error update failed: %v", msg.SubmissionID, updateErr)
					return queue.Retry
				}

				log.Printf("submission %s dead-lettered after permanent runner error: %v", msg.SubmissionID, err)
				return queue.DeadLetter
			}

			if retryErr := subRepo.IncrementRetryCount(context.Background(), msg.SubmissionID); retryErr != nil {
				log.Printf("submission %s retry_count update failed: %v", msg.SubmissionID, retryErr)
				return queue.Retry
			}

			sub.RetryCount++
			retryDecision := planJobResult(sub, err, maxRetries)
			if retryDecision == queue.Retry {
				log.Printf("submission %s retry %d/%d after runner error: %v", msg.SubmissionID, sub.RetryCount, maxRetries, err)
				sleepWithContext(ctx, time.Duration(sub.RetryCount)*time.Second)
				return queue.Retry
			}

			output := "runner failed after retries: " + err.Error()
			if updateErr := subRepo.UpdateResult(context.Background(), msg.SubmissionID, submission.StatusError, output, 0, 0); updateErr != nil {
				log.Printf("submission %s final error update failed: %v", msg.SubmissionID, updateErr)
				return queue.Retry
			}

			log.Printf("submission %s dead-lettered after %d retries: %v", msg.SubmissionID, sub.RetryCount, err)
			return queue.DeadLetter
		}

		if err := subRepo.UpdateResult(ctx, msg.SubmissionID, result.Verdict, result.Output, result.TotalTests, result.PassedTests); err != nil {
			log.Printf("submission %s result update failed: %v", msg.SubmissionID, err)
			return queue.Retry
		}

		log.Printf("submission %s verdict: %s", msg.SubmissionID, result.Verdict)

		return queue.Ack
	}
}

func trackJob(wg *sync.WaitGroup, inFlight *int64, handler func(context.Context, queue.SubmissionCreated) queue.HandlerResult) func(context.Context, queue.SubmissionCreated) queue.HandlerResult {
	return func(ctx context.Context, msg queue.SubmissionCreated) queue.HandlerResult {
		wg.Add(1)
		atomic.AddInt64(inFlight, 1)
		defer func() {
			atomic.AddInt64(inFlight, -1)
			wg.Done()
		}()

		return handler(ctx, msg)
	}
}

func sleepWithContext(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	runnerClient, closeRunner, err := runnerclient.Dial(cfg.RunnerAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer closeRunner()

	amqpConn, err := amqp.Dial(cfg.RabbitMQURL)
	if err != nil {
		log.Fatal(err)
	}
	defer amqpConn.Close()

	consumer, err := queue.NewConsumer(amqpConn)
	if err != nil {
		log.Fatal(err)
	}

	problemRepo := problem.NewPostgresRepository(db)
	submissionRepo := submission.NewPostgresRepository(db)

	log.Println("worker is running, waiting for jobs")
	var wg sync.WaitGroup
	var inFlight int64
	handler := handleJob(submissionRepo, problemRepo, runnerClient)

	if err := consumer.Consume(ctx, trackJob(&wg, &inFlight, handler)); err != nil {
		log.Fatal(err)
	}

	if ctx.Err() != nil {
		log.Printf("shutting down, waiting for %d in-flight jobs", atomic.LoadInt64(&inFlight))
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			log.Println("worker shutdown complete")
		case <-time.After(shutdownTimeout):
			log.Printf("worker shutdown timed out after %s", shutdownTimeout)
		}
	}
}
