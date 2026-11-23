package queue

import (
	"context"
	"os"
	"sync"
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
	for _, q := range []string{QueueSubmissionCreated, QueueSubmissionRetry, QueueSubmissionDead} {
		if _, err := ch.QueueDelete(q, false, false, false); err != nil {
			t.Fatal(err)
		}
	}
	return conn
}

func fastRetries(t *testing.T) {
	t.Helper()
	oldBase, oldMax := retryBaseDelay, retryMaxDelay
	retryBaseDelay, retryMaxDelay = 10*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { retryBaseDelay, retryMaxDelay = oldBase, oldMax })
}

func TestRetryDelay(t *testing.T) {
	oldBase, oldMax := retryBaseDelay, retryMaxDelay
	retryBaseDelay, retryMaxDelay = time.Second, 30*time.Second
	defer func() { retryBaseDelay, retryMaxDelay = oldBase, oldMax }()

	want := map[int]time.Duration{1: time.Second, 2: 2 * time.Second, 3: 4 * time.Second, 5: 16 * time.Second, 6: 30 * time.Second, 10: 30 * time.Second}
	for attempt, d := range want {
		if got := retryDelay(attempt); got != d {
			t.Errorf("retryDelay(%d) = %s, want %s", attempt, got, d)
		}
	}
}

func TestRetryIsCappedByDeliveryLimit(t *testing.T) {
	conn := dialTestBroker(t)
	fastRetries(t)

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

	var mu sync.Mutex
	calls := 0
	go consumer.Consume(ctx, func(context.Context, SubmissionCreated) HandlerResult {
		mu.Lock()
		calls++
		mu.Unlock()
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
			mu.Lock()
			t.Fatalf("message never dead-lettered, handler called %d times", calls)
		case <-time.After(100 * time.Millisecond):
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if calls > deliveryLimit+1 {
		t.Fatalf("handler called %d times, want at most %d", calls, deliveryLimit+1)
	}
}

func TestRetryDoesNotBlockOtherJobs(t *testing.T) {
	conn := dialTestBroker(t)
	oldBase := retryBaseDelay
	retryBaseDelay = 500 * time.Millisecond
	defer func() { retryBaseDelay = oldBase }()

	pub, err := NewPublisher(conn)
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := NewConsumer(conn)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if err := pub.Publish(context.Background(), SubmissionCreated{SubmissionID: id}); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var mu sync.Mutex
	var order []string
	var firstA, retriedA time.Time
	done := make(chan struct{})
	go consumer.Consume(ctx, func(_ context.Context, msg SubmissionCreated) HandlerResult {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, msg.SubmissionID)
		if msg.SubmissionID == "a" && firstA.IsZero() {
			firstA = time.Now()
			return Retry
		}
		if msg.SubmissionID == "a" {
			retriedA = time.Now()
			close(done)
		}
		return Ack
	})

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("retried job never came back, order = %v", order)
	}

	mu.Lock()
	defer mu.Unlock()
	// b is handled while a waits out its delay, instead of the worker sleeping on a
	if len(order) != 3 || order[0] != "a" || order[1] != "b" || order[2] != "a" {
		t.Fatalf("order = %v, want [a b a]", order)
	}
	if waited := retriedA.Sub(firstA); waited < retryBaseDelay {
		t.Fatalf("a came back after %s, want >= %s", waited, retryBaseDelay)
	}
}
