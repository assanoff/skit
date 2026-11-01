package rabbitmq

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	amqp "github.com/rabbitmq/amqp091-go"
	rmq "github.com/wagslane/go-rabbitmq"
	"go.opentelemetry.io/otel/trace"

	"github.com/assanoff/skit/broker"
)

// ConsumerConfig configures a Consumer.
type ConsumerConfig struct {
	// Queue is the queue to consume from (declared durable unless it already
	// exists). Required.
	Queue string
	// Exchange to bind the queue to. When set, the exchange is declared as a
	// durable topic exchange and the queue is bound with RoutingKeys.
	Exchange string
	// RoutingKeys bind the queue to the exchange (default ["#"] when Exchange
	// is set but no keys are given).
	RoutingKeys []string
	// Concurrency is the number of handler goroutines (default 1).
	Concurrency int
	// Name identifies the consumer in the supervisor, logs and metrics and as
	// the AMQP consumer tag (default Queue).
	Name string
	// Metrics is the registry for the skit_broker_* consumer metrics; nil —
	// metrics off.
	Metrics prometheus.Registerer
	// TracerProvider records the consumer span of every message; nil → the
	// global provider.
	TracerProvider trace.TracerProvider
}

// Consumer consumes CloudEvents messages from RabbitMQ and dispatches them to a
// broker.Handler. It implements worker.Runnable: Start blocks until the context
// is canceled (or Stop is called), then drains.
type Consumer struct {
	log     *slog.Logger
	cfg     ConsumerConfig
	handler broker.Handler
	rc      *rmq.Consumer
	once    sync.Once
}

// NewConsumer builds a Consumer of h. Every message h handles is a consumer
// span in the trace the message was created in and, with cfg.Metrics, a
// record in skit_broker_*; a panic of h requeues the message. Start declares
// the topology (exchange, queue, bindings) and consumes; until then a message
// published to the exchange has no queue to land in. Register it on a
// worker.Group like any other Runnable.
func NewConsumer(log *slog.Logger, conn *Conn, cfg ConsumerConfig, h broker.Handler) (*Consumer, error) {
	if cfg.Queue == "" {
		return nil, errors.New("rabbitmq: new consumer: queue is required")
	}
	cfg.Name = cmp.Or(cfg.Name, cfg.Queue)
	cfg.Concurrency = max(cfg.Concurrency, 1)

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
			rmq.WithConsumerOptionsExchangeKind("topic"),
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
	h = broker.Instrument(h, broker.InstrumentConfig{
		Consumer:       cfg.Name,
		System:         "rabbitmq",
		Metrics:        cfg.Metrics,
		TracerProvider: cfg.TracerProvider,
	})
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

	c.log.InfoContext(ctx, "rabbitmq consumer started", "queue", c.cfg.Queue, "consumer", c.cfg.Name)
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

// dispatch decodes one delivery into a broker.Message and runs the
// instrumented handler under panic recovery, mapping the broker.Action back to
// an rmq.Action. A malformed envelope is discarded (nack without requeue): requeuing it would
// loop forever, and a dead-letter exchange of the queue receives it.
func (c *Consumer) dispatch(ctx context.Context, d rmq.Delivery) rmq.Action {
	action := broker.Process(ctx, c.log, c.cfg.Name, d.Body, func(m *broker.Message) {
		m.Topic = d.Exchange
		m.Headers = Headers(d.Headers)
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

// Headers projects the header table of an AMQP delivery to
// broker.Message.Headers, dropping non-string values (CloudEvents and trace
// headers are strings). A consume loop of your own sets it on the message, so
// broker.Instrument continues the trace the message was created in.
func Headers(t amqp.Table) map[string]string {
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
