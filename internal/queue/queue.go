package queue

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// A quorum queue, so the broker itself caps redeliveries (x-delivery-limit).
// New name because RabbitMQ can't change an existing queue's type: the old
// classic "judge.submission.created" queue is just abandoned, and the stale
// submission sweeper re-publishes anything still stuck in it.
const QueueSubmissionCreated = "judge.submission.created.quorum"
const ExchangeDeadLetter = "judge.dlx"
const QueueSubmissionDead = "judge.submission.dead"
const RoutingKeySubmissionDead = "judge.submission.dead"

// QueueSubmissionRetry holds jobs waiting out their retry delay. Each message
// gets its own TTL; when it expires the broker dead-letters it back into
// QueueSubmissionCreated. Nothing consumes it directly.
const QueueSubmissionRetry = "judge.submission.retry"

// headerRetryAttempt counts retries on the republished copy. The quorum
// queue's own delivery count starts over for every copy, so this is what
// caps retries.
const headerRetryAttempt = "x-retry-attempt"

// deliveryLimit caps every Retry path (runner and DB errors alike) via
// headerRetryAttempt, and plain redeliveries (worker crash, shutdown) via the
// quorum queue's x-delivery-limit. Above the worker's 4 runner attempts, so it
// only kicks in for failures that keep repeating.
const deliveryLimit = 10

// retry delays double per attempt up to retryMaxDelay; vars so tests can
// shrink them
var (
	retryBaseDelay = time.Second
	retryMaxDelay  = 30 * time.Second
)

func retryDelay(attempt int) time.Duration {
	d := retryBaseDelay
	for i := 1; i < attempt && d < retryMaxDelay; i++ {
		d *= 2
	}
	return min(d, retryMaxDelay)
}

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

	if _, err := ch.QueueDeclare(QueueSubmissionCreated, true, false, false, false, amqp.Table{
		"x-queue-type":              "quorum",
		"x-delivery-limit":          deliveryLimit,
		"x-dead-letter-exchange":    ExchangeDeadLetter,
		"x-dead-letter-routing-key": RoutingKeySubmissionDead,
	}); err != nil {
		return err
	}

	// ponytail: per-message TTL only expires at the head of the queue, so a
	// short delay can wait behind a longer one (at most retryMaxDelay). One
	// queue per delay step if retry timing ever needs to be exact.
	_, err := ch.QueueDeclare(QueueSubmissionRetry, true, false, false, false, amqp.Table{
		"x-dead-letter-exchange":    "", // the default exchange routes by queue name
		"x-dead-letter-routing-key": QueueSubmissionCreated,
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
				c.retryLater(d, msg)
			default:
				d.Nack(false, false)
			}
		}
	}
}

// retryLater parks a copy of d in the retry queue and acks the original, so
// the worker moves on to the next job instead of sleeping through the delay.
// Not atomic: a crash in between redelivers the original as well, which the
// worker handles like any duplicate.
func (c *Consumer) retryLater(d amqp.Delivery, msg SubmissionCreated) {
	attempt := retryAttemptOf(d) + 1
	if attempt > deliveryLimit {
		log.Printf("submission %s dead-lettered after %d retries", msg.SubmissionID, deliveryLimit)
		d.Nack(false, false)
		return
	}

	delay := retryDelay(attempt)
	err := c.ch.PublishWithContext(context.Background(), "", QueueSubmissionRetry, false, false, amqp.Publishing{
		ContentType:  d.ContentType,
		DeliveryMode: amqp.Persistent,
		Body:         d.Body,
		Expiration:   strconv.FormatInt(delay.Milliseconds(), 10),
		Headers:      amqp.Table{headerRetryAttempt: int32(attempt)},
	})
	if err != nil {
		// still retried, just without the delay; the delivery limit caps this path
		log.Printf("submission %s: parking for retry failed, requeueing now: %v", msg.SubmissionID, err)
		d.Nack(false, true)
		return
	}
	d.Ack(false)
}

func retryAttemptOf(d amqp.Delivery) int {
	switch n := d.Headers[headerRetryAttempt].(type) {
	case int32:
		return int(n)
	case int64:
		return int(n)
	default:
		return 0
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
