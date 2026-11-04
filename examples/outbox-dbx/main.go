package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/assanoff/skit/broker/rabbitmq"
	"github.com/assanoff/skit/closer"
	"github.com/assanoff/skit/config"
	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/dbx/pg"
	"github.com/assanoff/skit/examples/outbox-dbx/orderapp"
	"github.com/assanoff/skit/examples/outbox-dbx/orderdb"
	"github.com/assanoff/skit/outbox"
	"github.com/assanoff/skit/queue"
	"github.com/assanoff/skit/worker"
)

// eventsQueue is the queue the events go to the broker through.
const eventsQueue = "events"

// options come from the flags and the environment.
type options struct {
	DB struct {
		User     string `long:"user"     env:"USER"     default:"postgres"       description:"postgres user"`
		Password string `long:"password" env:"PASSWORD" default:"postgres"       description:"postgres password"`
		Host     string `long:"host"     env:"HOST"     default:"localhost:5432" description:"postgres host:port"`
		Name     string `long:"name"     env:"NAME"     default:"postgres"       description:"postgres database"`
	} `group:"postgres" namespace:"db" env-namespace:"DB"`
	Broker struct {
		User     string `long:"user"     env:"USER"     default:"guest"     description:"rabbitmq user"`
		Password string `long:"password" env:"PASSWORD" default:"guest"     description:"rabbitmq password"`
		Host     string `long:"host"     env:"HOST"     default:"localhost" description:"rabbitmq host"`
		Port     string `long:"port"     env:"PORT"     default:"5672"      description:"rabbitmq port"`
	} `group:"broker" namespace:"broker" env-namespace:"BROKER"`
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.ErrorContext(context.Background(), "orders stopped", "err", err)
		os.Exit(1)
	}
}

// run shows what is built and what is run: newDeps builds the dependencies,
// the store and the domain sit on them, addEventWorkers registers the
// publishing of the events, and the group runs it.
func run(log *slog.Logger) error {
	var opts options
	if err := config.Parse(&opts); err != nil {
		if config.IsHelp(err) {
			return nil
		}
		return fmt.Errorf("options: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	d, err := newDeps(ctx, log, opts)
	if err != nil {
		return err
	}
	defer func() { _ = d.closer.CloseSync() }()

	store := orderdb.NewStore(log, d.DB, orderdb.WithTasks(d.Tasks))
	if err := store.EnsureSchema(ctx); err != nil {
		return err
	}
	core := orderapp.New(store)

	g := worker.NewGroup(log, worker.GroupConfig{ShutdownTimeout: 10 * time.Second})
	if err := addEventWorkers(log, g, d); err != nil {
		return err
	}

	if err := createAndPay(ctx, log, core); err != nil {
		return err
	}
	if err := g.Run(ctx); err != nil {
		return fmt.Errorf("group: %w", err)
	}
	return nil
}

// createAndPay creates one order, pays it and reads it back. Create schedules
// the event order.created; the workers publish it once they run.
func createAndPay(ctx context.Context, log *slog.Logger, core *orderapp.Core) error {
	order := orderapp.Order{ID: rand.Text(), Total: 1500}
	if err := core.Create(ctx, order); err != nil {
		return fmt.Errorf("create: %w", err)
	}
	if err := core.Pay(ctx, order.ID, orderapp.Payment{Amount: order.Total}); err != nil {
		return fmt.Errorf("pay: %w", err)
	}
	got, err := core.Get(ctx, order.ID)
	if err != nil {
		return fmt.Errorf("get: %w", err)
	}
	log.InfoContext(ctx, "order paid", "order_id", got.ID, "status", got.Status)
	return nil
}

// deps are the dependencies of the service; building them starts nothing.
type deps struct {
	DB       *dbx.DB
	Queue    *queue.PG       // every queue of the service, the events too
	Registry *queue.Registry // a Go type → its queue and kind
	// Publisher goes to outbox.Handler as a broker.Publisher.
	Publisher *rabbitmq.Publisher

	closer *closer.Closer
}

// newDeps opens the pool, the queue and its registry, and the publisher. On
// an error it closes what it has opened.
func newDeps(ctx context.Context, log *slog.Logger, opts options) (*deps, error) {
	d := &deps{closer: closer.New()}
	if err := d.build(ctx, log, opts); err != nil {
		_ = d.closer.CloseSync()
		return nil, err
	}
	return d, nil
}

func (d *deps) build(ctx context.Context, log *slog.Logger, opts options) error {
	db, err := pg.Open(pg.Config{
		User:       opts.DB.User,
		Password:   opts.DB.Password,
		Host:       opts.DB.Host,
		Name:       opts.DB.Name,
		DisableTLS: true,
	})
	if err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	d.closer.Add(db.Close)
	d.DB = db

	// The table queue_tasks, under the advisory lock of package queue: safe
	// for many replicas.
	d.Queue = queue.NewPG(log, db)
	if err = d.Queue.EnsureSchema(ctx); err != nil {
		return fmt.Errorf("queue: %w", err)
	}

	// The event goes to the queue of events in the envelope of outbox: its id,
	// time, data and the trace of the request that made it.
	d.Registry = queue.NewRegistry()
	err = d.Registry.Register[orderapp.OrderCreated](eventsQueue, "order.created",
		queue.WithEncoder(outbox.JSON))
	if err != nil {
		return fmt.Errorf("registry: %w", err)
	}

	conn, err := rabbitmq.Dial(rabbitmq.Config{
		User:     opts.Broker.User,
		Password: opts.Broker.Password,
		Host:     opts.Broker.Host,
		Port:     opts.Broker.Port,
	})
	if err != nil {
		return fmt.Errorf("rabbitmq: %w", err)
	}
	d.closer.Add(conn.Close)
	// The exchange is the topic (orders), the routing key the type of the
	// event (order.created); rabbitmq.WithRoutingKey routes otherwise.
	pub, err := rabbitmq.NewPublisher(conn, rabbitmq.PublisherConfig{Source: "orders"})
	if err != nil {
		return fmt.Errorf("publisher: %w", err)
	}
	d.closer.Add(pub.Close)
	d.Publisher = pub
	return nil
}

// Tasks is the one place where the assembly decides where the tasks of a
// transaction go: to the queues of tx, by the routes of the registry.
func (d *deps) Tasks(tx *dbx.Tx) *queue.Scheduler {
	return queue.Bind(tx, d.Queue, d.Registry)
}

// addEventWorkers registers the publishing of the events scheduled inside a
// business transaction: the workers of the queue of events, the cleaner of
// its dead letters and the poll of its backlog.
func addEventWorkers(log *slog.Logger, g *worker.Group, d *deps) error {
	// The handler sees only a broker.Publisher; RabbitMQ is unknown to it.
	mux := queue.NewMux()
	if err := mux.Register("order.created", outbox.Handler(d.Publisher, "orders")); err != nil {
		return fmt.Errorf("mux: %w", err)
	}
	// A kind without a handler would go to the dead letters: check at start.
	if err := d.Registry.Check(eventsQueue, mux); err != nil {
		return fmt.Errorf("routes: %w", err)
	}
	ws, err := queue.Workers(log, d.Queue, outbox.Config(eventsQueue), mux.Handle)
	if err != nil {
		return fmt.Errorf("workers: %w", err)
	}
	g.Add(ws...)
	return nil
}
