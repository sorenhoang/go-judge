package main

import (
	"context"
	"database/sql"
	"log"

	_ "github.com/jackc/pgx/v5/stdlib"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/sorenhoang/go-judge/internal/config"
	"github.com/sorenhoang/go-judge/internal/problem"
	"github.com/sorenhoang/go-judge/internal/queue"
	"github.com/sorenhoang/go-judge/internal/runnerclient"
	"github.com/sorenhoang/go-judge/internal/submission"
)

func handleJob(subRepo submission.Repository, probRepo problem.Repository, runner *runnerclient.Client) func(queue.SubmissionCreated) error {
	return func(msg queue.SubmissionCreated) error {
		ctx := context.Background()

		log.Printf("received job for submission %s", msg.SubmissionID)

		if err := subRepo.UpdateStatus(ctx, msg.SubmissionID, submission.StatusRunning); err != nil {
			return err
		}

		sub, err := subRepo.GetByID(ctx, msg.SubmissionID)
		if err != nil {
			return err
		}

		prob, err := probRepo.Get(ctx, msg.ProblemID)
		if err != nil {
			return err
		}

		result, err := runner.Run(ctx, sub.Code, prob.TestCode)
		if err != nil {
			return err
		}

		if err := subRepo.UpdateResult(ctx, msg.SubmissionID, result.Verdict, result.Output, result.TotalTests, result.PassedTests); err != nil {
			return err
		}

		log.Printf("submission %s verdict: %s", msg.SubmissionID, result.Verdict)

		return nil
	}
}

func main() {
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
	if err := consumer.Consume(handleJob(submissionRepo, problemRepo, runnerClient)); err != nil {
		log.Fatal(err)
	}
}
