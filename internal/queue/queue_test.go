package queue

import (
	"context"
	"os"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Needs a throwaway RabbitMQ, e.g.
// docker run -d --rm -p 55672:5672 rabbitmq:3-management
// TEST_RABBITMQ_URL=amqp://guest:guest@localhost:55672/ go test ./internal/queue
func dialTestBroker(t *testing.T) *amqp.Connection {
	t.Helper()
	url := os.Getenv("TEST_RABBITMQ_URL")
	if url == "" {
		t.Skip("TEST_RABBITMQ_URL not set")
	}
	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()
	for _, q := range []string{QueueSubmissionCreated, QueueSubmissionDead} {
		if _, err := ch.QueueDelete(q, false, false, false); err != nil {
			t.Fatal(err)
		}
	}
	return conn
}

func TestRetryIsCappedByDeliveryLimit(t *testing.T) {
	conn := dialTestBroker(t)

	pub, err := NewPublisher(conn)
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := NewConsumer(conn)
	if err != nil {
		t.Fatal(err)
	}

	if err := pub.Publish(context.Background(), SubmissionCreated{SubmissionID: "poison", ProblemID: "p"}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	calls := 0
	go consumer.Consume(ctx, func(context.Context, SubmissionCreated) HandlerResult {
		calls++
		return Retry // a failure that never goes away
	})

	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()

	for {
		q, err := ch.QueueDeclarePassive(QueueSubmissionDead, true, false, false, false, nil)
		if err != nil {
			t.Fatal(err)
		}
		if q.Messages == 1 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("message never dead-lettered, handler called %d times", calls)
		case <-time.After(100 * time.Millisecond):
		}
	}

	if calls > deliveryLimit+1 {
		t.Fatalf("handler called %d times, want at most %d", calls, deliveryLimit+1)
	}
}
