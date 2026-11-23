package queue

import (
	"context"
	"encoding/json"
	"errors"
	"log"

	amqp "github.com/rabbitmq/amqp091-go"
)

const QueueSubmissionCreated = "judge.submission.created"
const ExchangeDeadLetter = "judge.dlx"
const QueueSubmissionDead = "judge.submission.dead"
const RoutingKeySubmissionDead = "judge.submission.dead"

type HandlerResult int

const (
	Ack HandlerResult = iota
	Retry
	DeadLetter
)

type SubmissionCreated struct {
	SubmissionID string `json:"submission_id"`
	ProblemID    string `json:"problem_id"`
}

type Publisher struct {
	ch *amqp.Channel
}

func NewPublisher(conn *amqp.Connection) (*Publisher, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, err
	}

	if err := declareTopology(ch); err != nil {
		return nil, err
	}

	return &Publisher{ch: ch}, nil
}

func (p *Publisher) Publish(ctx context.Context, msg SubmissionCreated) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return p.ch.PublishWithContext(ctx, "", QueueSubmissionCreated, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	})
}

type Consumer struct {
	ch *amqp.Channel
}

func NewConsumer(conn *amqp.Connection) (*Consumer, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, err
	}

	if err := declareTopology(ch); err != nil {
		return nil, err
	}

	if err := ch.Qos(1, 0, false); err != nil {
		return nil, err
	}

	return &Consumer{ch: ch}, nil
}

func declareTopology(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(ExchangeDeadLetter, "direct", true, false, false, false, nil); err != nil {
		return err
	}

	if _, err := ch.QueueDeclare(QueueSubmissionDead, true, false, false, false, nil); err != nil {
		return err
	}

	if err := ch.QueueBind(QueueSubmissionDead, RoutingKeySubmissionDead, ExchangeDeadLetter, false, nil); err != nil {
		return err
	}

	_, err := ch.QueueDeclare(QueueSubmissionCreated, true, false, false, false, amqp.Table{
		"x-dead-letter-exchange":    ExchangeDeadLetter,
		"x-dead-letter-routing-key": RoutingKeySubmissionDead,
	})

	return err
}

// Consume runs handler for each delivery until ctx is cancelled. Cancelling ctx
// only stops taking new jobs: the in-flight handler gets a context that isn't
// cancelled, so it runs to completion and is acked instead of aborted mid-run.
func (c *Consumer) Consume(ctx context.Context, handler func(context.Context, SubmissionCreated) HandlerResult) error {
	const consumerTag = "go-judge-worker"

	deliveries, err := c.ch.Consume(QueueSubmissionCreated, consumerTag, false, false, false, false, nil)
	if err != nil {
		return err
	}

	jobCtx := context.WithoutCancel(ctx)

	for {
		// checked first so a delivery that's ready at the same time doesn't win the select
		if ctx.Err() != nil {
			return c.stop(consumerTag, deliveries)
		}

		select {
		case <-ctx.Done():
			return c.stop(consumerTag, deliveries)
		case d, ok := <-deliveries:
			if !ok {
				return errors.New("delivery channel closed (RabbitMQ connection lost)")
			}

			var msg SubmissionCreated
			if err := json.Unmarshal(d.Body, &msg); err != nil {
				log.Printf("dead-lettering undecodable message: %v: %q", err, d.Body)
				d.Nack(false, false)
				continue
			}

			switch handler(jobCtx, msg) {
			case Ack:
				d.Ack(false)
			case Retry:
				d.Nack(false, true)
			default:
				d.Nack(false, false)
			}
		}
	}
}

func (c *Consumer) stop(consumerTag string, deliveries <-chan amqp.Delivery) error {
	if err := c.ch.Cancel(consumerTag, false); err != nil {
		return err
	}
	for d := range deliveries {
		d.Nack(false, true)
	}
	return nil
}
