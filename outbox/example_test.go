package outbox_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/broker/rabbitmq"
	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/dbx/pg"
	"github.com/assanoff/skit/metrics"
	"github.com/assanoff/skit/outbox"
	"github.com/assanoff/skit/queue"
	"github.com/assanoff/skit/worker"
)

// widgetCreated is an event of the widget domain.
type widgetCreated struct {
	ID string `json:"id"`
}

// The wiring of a queue of events: the Registry routes the event type to the
// queue, the Mux publishes it to its topic, and the workers of the queue run
// until a signal. The store schedules the event in the transaction of its
// change. The example needs Postgres and RabbitMQ, so go test builds it but
// does not run it.
func Example() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log := slog.Default()

	db, err := pg.Open(pg.Config{Host: "localhost:5432", Name: "shop", DisableTLS: true})
	if err != nil {
		log.ErrorContext(ctx, "open", "err", err)
		return
	}
	defer func() { _ = db.Close() }()
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

	q := queue.NewPG(log, db)
	if err := q.EnsureSchema(ctx); err != nil {
		log.ErrorContext(ctx, "schema", "err", err)
		return
	}
	reg := queue.NewRegistry()
	mux := queue.NewMux()
	err = errors.Join(
		reg.Register[widgetCreated]("events", "widget.created", queue.WithEncoder(outbox.JSON)),
		mux.Register("widget.created", outbox.Handler(pub, "widgets")),
	)
	if err == nil {
		err = reg.Check("events", mux) // every event has a handler
	}
	if err != nil {
		log.ErrorContext(ctx, "routes", "err", err)
		return
	}

	// The store of the domain, within the transaction of its change.
	err = dbx.WithinTran(ctx, log, db, func(tx *dbx.Tx) error {
		const ins = `INSERT INTO widgets (id, name) VALUES (:id, :name)`
		err := dbx.NamedExec(ctx, tx, ins, map[string]any{"id": "w-1", "name": "gear"})
		if err != nil {
			return err
		}
		return queue.Bind(tx, q, reg).Schedule(ctx, widgetCreated{ID: "w-1"})
	})
	if err != nil {
		log.ErrorContext(ctx, "create widget", "err", err)
		return
	}

	workers, err := queue.Workers(log, q, outbox.Config("events"), mux.Handle,
		queue.WithMetrics(metrics.New()))
	if err != nil {
		log.ErrorContext(ctx, "workers", "err", err)
		return
	}
	g := worker.NewGroup(log, worker.GroupConfig{ShutdownTimeout: 10 * time.Second})
	g.Add(workers...)
	if err := g.Run(ctx); err != nil {
		log.ErrorContext(ctx, "group stopped", "err", err)
	}
}

// publisherFunc is a broker.Publisher of a function, in place of an adapter.
type publisherFunc func(ctx context.Context, m broker.Message) error

func (f publisherFunc) Publish(ctx context.Context, m broker.Message) error {
	return f(ctx, m)
}

// JSON writes the event into the Payload of its task, and Handler publishes
// the task to the topic as a message of the type of its kind. The error of
// the publisher decides the outcome of the task: a broker.Permanent one sends
// it to the dead letters (queue.Permanent), any other retries it and stops the
// batch (queue.Halt).
func ExampleHandler() {
	ctx := context.Background()
	payload, err := outbox.JSON(ctx, widgetCreated{ID: "w-1"})
	if err != nil {
		fmt.Println(err)
		return
	}
	task := queue.Task{Queue: "events", Kind: "widget.created", Payload: payload}

	var publish publisherFunc = func(_ context.Context, m broker.Message) error {
		fmt.Println(m.Type, m.Topic, m.ContentType, string(m.Data))
		return nil
	}
	fmt.Println(outbox.Handler(publish, "widgets")(ctx, task))

	publish = func(context.Context, broker.Message) error {
		return errors.New("connection refused") // retried
	}
	fmt.Println(outbox.Handler(publish, "widgets")(ctx, task))

	publish = func(context.Context, broker.Message) error {
		return broker.Permanent(errors.New("message over 1 MiB")) // dead letter
	}
	fmt.Println(outbox.Handler(publish, "widgets")(ctx, task))
	// Output:
	// widget.created widgets application/json {"id":"w-1"}
	// <nil>
	// connection refused
	// message over 1 MiB
}
