package queue

import (
	"context"
	"encoding/json"

	amqp "github.com/rabbitmq/amqp091-go"
)

const QueueSubmissionCreated = "judge.submission.created"

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

	if _, err := ch.QueueDeclare(QueueSubmissionCreated, true, false, false, false, nil); err != nil {
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

	if _, err := ch.QueueDeclare(QueueSubmissionCreated, true, false, false, false, nil); err != nil {
		return nil, err
	}

	if err := ch.Qos(1, 0, false); err != nil {
		return nil, err
	}

	return &Consumer{ch: ch}, nil
}

func (c *Consumer) Consume(handler func(SubmissionCreated) error) error {
	deliveries, err := c.ch.Consume(QueueSubmissionCreated, "", false, false, false, false, nil)
	if err != nil {
		return err
	}

	for d := range deliveries {
		var msg SubmissionCreated
		if err := json.Unmarshal(d.Body, &msg); err != nil {
			d.Nack(false, false)
			continue
		}

		if err := handler(msg); err != nil {
			d.Nack(false, false)
			continue
		}

		d.Ack(false)
	}

	return nil
}
