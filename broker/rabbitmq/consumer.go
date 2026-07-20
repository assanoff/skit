package rabbitmq

import (
	"context"
	"fmt"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"
	rmq "github.com/wagslane/go-rabbitmq"

	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/logger"
)

// ConsumerConfig configures a Consumer.
type ConsumerConfig struct {
	// Queue is the queue to consume from (declared durable unless it already
	// exists). Required.
	Queue string
	// Exchange to bind the queue to. When set, the exchange is declared
	// (durable topic by default) and the queue is bound with RoutingKeys.
	Exchange string
	// ExchangeKind defaults to "topic".
	ExchangeKind string
	// RoutingKeys bind the queue to the exchange (default ["#"] when Exchange
	// is set but no keys are given).
	RoutingKeys []string
	// Concurrency is the number of handler goroutines (default 1).
	Concurrency int
	// Name identifies the consumer in the supervisor/logs and as the AMQP
	// consumer tag (default "consumer").
	Name string
}

// Consumer consumes CloudEvents messages from RabbitMQ and dispatches them to a
// broker.Handler. It implements worker.Runnable: Start blocks until the context
// is canceled (or Stop is called), then drains.
type Consumer struct {
	log     *logger.Logger
	cfg     ConsumerConfig
	handler broker.Handler
	rc      *rmq.Consumer
	once    sync.Once
}

// New builds a Consumer from a transport-neutral broker.Subscription, mapping it
// onto RabbitMQ concepts (Topic → exchange, Group → queue, RoutingKeys →
// bindings). It is the pluggable entry point every transport adapter shares
// (kafka.New, nats.New would have the same shape), so wiring code can depend on
// broker.Subscription instead of the RabbitMQ-specific ConsumerConfig. Use
// NewConsumer directly only when you need RabbitMQ-only options (e.g.
// ExchangeKind).
func New(conn *Conn, log *logger.Logger, sub broker.Subscription, h broker.Handler) (*Consumer, error) {
	return NewConsumer(conn, log, ConsumerConfig{
		Queue:       sub.Group,
		Exchange:    sub.Topic,
		RoutingKeys: sub.Filters,
		Concurrency: sub.Concurrency,
		Name:        sub.Name,
	}, h)
}

// NewConsumer declares the topology and builds a Consumer. Register it on a
// worker.Group like any other Runnable.
func NewConsumer(conn *Conn, log *logger.Logger, cfg ConsumerConfig, h broker.Handler) (*Consumer, error) {
	if cfg.Queue == "" {
		return nil, fmt.Errorf("rabbitmq: new consumer: queue is required")
	}
	if cfg.Name == "" {
		cfg.Name = "consumer"
	}
	if cfg.ExchangeKind == "" {
		cfg.ExchangeKind = "topic"
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 1
	}

	opts := []func(*rmq.ConsumerOptions){
		rmq.WithConsumerOptionsConsumerName(cfg.Name),
		rmq.WithConsumerOptionsQueueDurable,
		rmq.WithConsumerOptionsConcurrency(cfg.Concurrency),
	}
	if cfg.Exchange != "" {
		keys := cfg.RoutingKeys
		if len(keys) == 0 {
			keys = []string{"#"}
		}
		opts = append(
			opts,
			rmq.WithConsumerOptionsExchangeName(cfg.Exchange),
			rmq.WithConsumerOptionsExchangeKind(cfg.ExchangeKind),
			rmq.WithConsumerOptionsExchangeDeclare,
			rmq.WithConsumerOptionsExchangeDurable,
		)
		for _, k := range keys {
			opts = append(opts, rmq.WithConsumerOptionsRoutingKey(k))
		}
	}

	rc, err := rmq.NewConsumer(conn.Conn, cfg.Queue, opts...)
	if err != nil {
		return nil, fmt.Errorf("rabbitmq: new consumer: %w", err)
	}
	return &Consumer{log: log, cfg: cfg, handler: h, rc: rc}, nil
}

// Name identifies the consumer in the supervisor and logs.
func (c *Consumer) Name() string { return c.cfg.Name }

// Start consumes until ctx is canceled (or Stop closes the consumer). Run
// blocks with automatic reconnection; a watcher goroutine closes it on cancel.
func (c *Consumer) Start(ctx context.Context) error {
	// The watcher closes the consumer on ctx cancel. It also unblocks when Start
	// returns for any other reason (Run errored / reconnection exhausted), so the
	// goroutine never parks on a context that outlives this Run.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			c.close()
		case <-done:
		}
	}()

	c.log.Info(ctx, "rabbitmq consumer started", "queue", c.cfg.Queue, "consumer", c.cfg.Name)
	err := c.rc.Run(func(d rmq.Delivery) rmq.Action {
		return c.dispatch(ctx, d)
	})
	// Run returns when the consumer is closed; a context-driven shutdown is
	// graceful, not an error.
	if err != nil && ctx.Err() == nil {
		return fmt.Errorf("rabbitmq consumer %q: %w", c.cfg.Name, err)
	}
	return nil
}

// Stop closes the consumer, unblocking Start.
func (c *Consumer) Stop(context.Context) error {
	c.close()
	return nil
}

func (c *Consumer) close() {
	c.once.Do(func() { c.rc.Close() })
}

// dispatch decodes one delivery into a broker.Message and runs the handler
// under panic recovery, mapping the broker.Action back to an rmq.Action. A
// malformed envelope is Ack'd (dropped) — requeuing it would loop forever.
func (c *Consumer) dispatch(ctx context.Context, d rmq.Delivery) rmq.Action {
	action := broker.Process(ctx, c.log.Slog(), c.cfg.Name, d.Body, func(m *broker.Message) {
		m.Topic = d.Exchange
		m.Key = d.RoutingKey
		m.Headers = stringHeaders(d.Headers)
	}, c.handler)
	return ToAction(action)
}

// ToAction maps a broker.Action onto the go-rabbitmq delivery action, so a
// hand-written or scaffolded consumer loop can reuse the same mapping skit's
// Consumer uses.
func ToAction(a broker.Action) rmq.Action {
	switch a {
	case broker.Requeue:
		return rmq.NackRequeue
	case broker.Discard:
		return rmq.NackDiscard
	default:
		return rmq.Ack
	}
}

// stringHeaders projects an AMQP header table to string→string, dropping
// non-string values (CloudEvents/trace headers are strings).
func stringHeaders(t amqp.Table) map[string]string {
	if len(t) == 0 {
		return nil
	}
	out := make(map[string]string, len(t))
	for k, v := range t {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}
