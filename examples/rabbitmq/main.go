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
	"github.com/assanoff/skit/config"
	"github.com/assanoff/skit/examples/rabbitmq/orderapp"
	"github.com/assanoff/skit/worker"
)

// options come from the flags and the environment.
type options struct {
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
// addConsumers registers the consumers, and the group runs them.
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

	d, err := newDeps(rabbitmq.Config{
		User:     opts.Broker.User,
		Password: opts.Broker.Password,
		Host:     opts.Broker.Host,
		Port:     opts.Broker.Port,
	})
	if err != nil {
		return err
	}
	defer d.close()

	app := orderapp.New(log, d.Publisher)
	g := worker.NewGroup(log, worker.GroupConfig{ShutdownTimeout: 10 * time.Second})
	if err := addConsumers(log, g, d, app); err != nil {
		return err
	}

	// One order to show the publishing side.
	order := orderapp.Order{ID: rand.Text(), Total: 1500}
	if err := app.Create(ctx, order); err != nil {
		return err
	}
	log.InfoContext(ctx, "order created", "order_id", order.ID)

	if err := g.Run(ctx); err != nil {
		return fmt.Errorf("group: %w", err)
	}
	return nil
}

// deps are the dependencies of the service; building them starts nothing.
type deps struct {
	Conn *rabbitmq.Conn
	// Publisher goes to the domain as a broker.Publisher.
	Publisher *rabbitmq.Publisher
}

// newDeps connects to RabbitMQ and builds the publisher of the events.
func newDeps(cfg rabbitmq.Config) (*deps, error) {
	conn, err := rabbitmq.Dial(cfg)
	if err != nil {
		return nil, fmt.Errorf("rabbitmq: %w", err)
	}
	pub, err := rabbitmq.NewPublisher(conn, rabbitmq.PublisherConfig{Source: "orders"})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("publisher: %w", err)
	}
	return &deps{Conn: conn, Publisher: pub}, nil
}

// close closes the publisher, then the connection it uses.
func (d *deps) close() {
	_ = d.Publisher.Close()
	_ = d.Conn.Close()
}

// addConsumers registers the consumers of the incoming events in g. Start
// declares the exchange payments, the queue orders.payments and its binding.
func addConsumers(log *slog.Logger, g *worker.Group, d *deps, app *orderapp.App) error {
	cons, err := rabbitmq.NewConsumer(log, d.Conn, rabbitmq.ConsumerConfig{
		Queue:       "orders.payments",
		Exchange:    "payments",
		RoutingKeys: []string{"payment.paid"},
	}, app.HandlePaid)
	if err != nil {
		return fmt.Errorf("consumer: %w", err)
	}
	g.Add(cons)
	return nil
}
