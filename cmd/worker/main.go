package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"os/signal"
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
const submissionTimeout = 45 * time.Second // headroom above judge.sandboxTimeout (30s) so the sandbox's own timeout fires first

// backoffUnit spaces out retries so a requeued job doesn't hot-loop while
// Postgres or the runner is down. A var so tests can zero it.
var backoffUnit = time.Second

// retryLater waits before requeueing so a failure that keeps happening (e.g.
// Postgres down) doesn't spin; the queue's delivery limit caps the total.
func retryLater(format string, args ...any) queue.HandlerResult {
	log.Printf(format+", retrying", args...)
	time.Sleep(backoffUnit)
	return queue.Retry
}

type runner interface {
	Run(ctx context.Context, code string, testCode string) (runnerclient.Result, error)
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

	// RetryCount counts failed attempts, so this allows maxRetries retries
	if isRetryableRunnerError(err) && sub.RetryCount <= maxRetries {
		return queue.Retry
	}

	return queue.DeadLetter
}

func handleJob(subRepo submission.Repository, probRepo problem.Repository, runner runner) func(context.Context, queue.SubmissionCreated) queue.HandlerResult {
	return func(ctx context.Context, msg queue.SubmissionCreated) queue.HandlerResult {
		log.Printf("received job for submission %s", msg.SubmissionID)

		sub, err := subRepo.GetByID(ctx, msg.SubmissionID)
		if errors.Is(err, submission.ErrNotFound) {
			log.Printf("submission %s does not exist, dead-lettering", msg.SubmissionID)
			return queue.DeadLetter
		}
		if err != nil {
			return retryLater("submission %s lookup failed: %v", msg.SubmissionID, err)
		}

		if sub.Status.IsTerminal() {
			log.Printf("submission %s already finished with status %s, acking", msg.SubmissionID, sub.Status)
			return queue.Ack
		}

		if err := subRepo.UpdateStatus(ctx, msg.SubmissionID, submission.StatusRunning); err != nil {
			return retryLater("submission %s status update failed: %v", msg.SubmissionID, err)
		}

		prob, err := probRepo.Get(ctx, msg.ProblemID)
		if errors.Is(err, problem.ErrNotFound) {
			if updateErr := subRepo.UpdateResult(ctx, msg.SubmissionID, submission.StatusError, "problem "+msg.ProblemID+" not found", 0, 0); updateErr != nil {
				return retryLater("submission %s error update failed: %v", msg.SubmissionID, updateErr)
			}
			log.Printf("problem %s not found for submission %s, dead-lettering", msg.ProblemID, msg.SubmissionID)
			return queue.DeadLetter
		}
		if err != nil {
			return retryLater("problem %s lookup failed for submission %s: %v", msg.ProblemID, msg.SubmissionID, err)
		}

		jobCtx, cancel := context.WithTimeout(ctx, submissionTimeout)
		defer cancel()

		result, err := runner.Run(jobCtx, sub.Code, prob.TestCode)
		if err != nil {
			if !isRetryableRunnerError(err) {
				output := "runner failed permanently: " + err.Error()
				if updateErr := subRepo.UpdateResult(ctx, msg.SubmissionID, submission.StatusError, output, 0, 0); updateErr != nil {
					return retryLater("submission %s permanent error update failed: %v", msg.SubmissionID, updateErr)
				}

				log.Printf("submission %s dead-lettered after permanent runner error: %v", msg.SubmissionID, err)
				return queue.DeadLetter
			}

			if retryErr := subRepo.IncrementRetryCount(ctx, msg.SubmissionID); retryErr != nil {
				return retryLater("submission %s retry_count update failed: %v", msg.SubmissionID, retryErr)
			}

			sub.RetryCount++
			retryDecision := planJobResult(sub, err, maxRetries)
			if retryDecision == queue.Retry {
				log.Printf("submission %s retry %d/%d after runner error: %v", msg.SubmissionID, sub.RetryCount, maxRetries, err)
				time.Sleep(time.Duration(sub.RetryCount) * backoffUnit)
				return queue.Retry
			}

			output := "runner failed after retries: " + err.Error()
			if updateErr := subRepo.UpdateResult(ctx, msg.SubmissionID, submission.StatusError, output, 0, 0); updateErr != nil {
				return retryLater("submission %s final error update failed: %v", msg.SubmissionID, updateErr)
			}

			log.Printf("submission %s dead-lettered after %d retries: %v", msg.SubmissionID, sub.RetryCount-1, err)
			return queue.DeadLetter
		}

		if err := subRepo.UpdateResult(ctx, msg.SubmissionID, result.Verdict, result.Output, result.TotalTests, result.PassedTests); err != nil {
			return retryLater("submission %s result update failed: %v", msg.SubmissionID, err)
		}

		log.Printf("submission %s verdict: %s", msg.SubmissionID, result.Verdict)

		return queue.Ack
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

	// Consume returns once ctx is cancelled and the in-flight job (if any) has finished
	if err := consumer.Consume(ctx, handleJob(submissionRepo, problemRepo, runnerClient)); err != nil {
		log.Fatal(err)
	}

	log.Println("worker shutdown complete")
}
