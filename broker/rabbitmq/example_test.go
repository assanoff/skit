package rabbitmq_test

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/broker/rabbitmq"
	"github.com/assanoff/skit/metrics"
	"github.com/assanoff/skit/worker"
)

// A consumer service: the consumer binds its queue to the event types it
// handles, and the Group runs it until a signal. Start declares the exchange,
// the queue and its bindings; until then a message published to the exchange
// has no queue to land in. The examples need RabbitMQ, so go test builds them
// but does not run them.
func Example() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log := slog.Default()

	conn, err := rabbitmq.Dial(rabbitmq.Config{
		User: "guest", Password: "guest", Host: "localhost", Port: "5672",
	})
	if err != nil {
		log.ErrorContext(ctx, "dial", "err", err)
		return
	}
	defer func() { _ = conn.Close() }()

	cons, err := rabbitmq.NewConsumer(log, conn, rabbitmq.ConsumerConfig{
		Queue:       "widgets.billing",
		Exchange:    "widgets",
		RoutingKeys: []string{"widget.created"},
		Metrics:     metrics.New(),
	}, func(ctx context.Context, m broker.Message) broker.Action {
		log.InfoContext(ctx, "widget created", "id", m.ID, "data", string(m.Data))
		return broker.Ack
	})
	if err != nil {
		log.ErrorContext(ctx, "consumer", "err", err)
		return
	}

	g := worker.NewGroup(log, worker.GroupConfig{ShutdownTimeout: 10 * time.Second})
	g.Add(cons)
	if err := g.Run(ctx); err != nil {
		log.ErrorContext(ctx, "group stopped", "err", err)
	}
}

// Publish sends a message to the exchange of its Topic under the routing key
// of its Type and returns once the broker confirms it. One connection serves
// every publisher and consumer of the process.
func ExampleNewPublisher() {
	ctx := context.Background()
	log := slog.Default()
	conn, err := rabbitmq.Dial(rabbitmq.Config{
		User: "guest", Password: "guest", Host: "localhost", Port: "5672",
	})
	if err != nil {
		log.ErrorContext(ctx, "dial", "err", err)
		return
	}
	defer func() { _ = conn.Close() }()

	pub, err := rabbitmq.NewPublisher(conn, rabbitmq.PublisherConfig{Source: "shop"})
	if err != nil {
		log.ErrorContext(ctx, "publisher", "err", err)
		return
	}
	defer func() { _ = pub.Close() }()

	err = pub.Publish(ctx, broker.Message{
		Type:        "widget.created", // the routing key
		ContentType: "application/json",
		Data:        []byte(`{"id":"w-1"}`),
		Topic:       "widgets", // the exchange
	})
	if err != nil {
		log.ErrorContext(ctx, "publish", "err", err)
	}
}

// WithRoutingKey derives the routing key from the whole message: here, for a
// queue of another system bound to keys of its own.
func ExampleWithRoutingKey() {
	ctx := context.Background()
	log := slog.Default()
	conn, err := rabbitmq.Dial(rabbitmq.Config{
		User: "guest", Password: "guest", Host: "localhost", Port: "5672",
	})
	if err != nil {
		log.ErrorContext(ctx, "dial", "err", err)
		return
	}
	defer func() { _ = conn.Close() }()

	pub, err := rabbitmq.NewPublisher(conn, rabbitmq.PublisherConfig{Source: "shop"},
		rabbitmq.WithRoutingKey(func(m broker.Message) string {
			return "legacy." + m.Type
		}))
	if err != nil {
		log.ErrorContext(ctx, "publisher", "err", err)
		return
	}
	defer func() { _ = pub.Close() }()

	// Routed as legacy.widget.created.
	err = pub.Publish(ctx, broker.Message{
		Type:  "widget.created",
		Data:  []byte(`{"id":"w-1"}`),
		Topic: "widgets",
	})
	if err != nil {
		log.ErrorContext(ctx, "publish", "err", err)
	}
}
