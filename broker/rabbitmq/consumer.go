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
	"github.com/assanoff/skit/broker/internal/consume"
)

// RoutingKeyHeader is the header a Consumer puts the routing key of a delivery
// in. A publisher derives a key of its own (WithRoutingKey) and sends this
// header on as an AMQP header, like any other header of the message.
const RoutingKeyHeader = "routing-key"

// ConsumerConfig configures a Consumer.
type ConsumerConfig struct {
	// Queue is the queue to consume from (declared durable unless it already
	// exists). Required.
	Queue string
	// Exchange to bind the queue to. When set, the exchange is declared as a
	// durable exchange of ExchangeKind and the queue is bound with RoutingKeys.
	Exchange string
	// ExchangeKind is the kind of the exchange: topic (default), direct,
	// fanout or headers.
	ExchangeKind string
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

// Consumer consumes messages from RabbitMQ, unpacks them with its envelope and
// dispatches them to a broker.Handler. It implements worker.Runnable: Start
// blocks until the context is canceled (or Stop is called), then drains.
type Consumer struct {
	log     *slog.Logger
	cfg     ConsumerConfig
	env     broker.Envelope
	handler broker.Handler
	rc      *rmq.Consumer
	once    sync.Once
}

// NewConsumer builds a Consumer of h. Every message h handles is a consumer
// span in the trace the message was created in and, with cfg.Metrics, a
// record in skit_broker_*; a panic of h requeues the message. Start declares
// the topology (exchange, queue, bindings) and consumes; until then a message
// published to the exchange has no queue to land in. Register it on a
// worker.Group like any other Runnable. The envelope is broker.CloudEvents{}
// unless WithEnvelope sets another.
func NewConsumer(log *slog.Logger, conn *Conn, cfg ConsumerConfig, h broker.Handler, opts ...ConsumerOption) (*Consumer, error) {
	if cfg.Queue == "" {
		return nil, errors.New("rabbitmq: new consumer: queue is required")
	}
	cfg.Name = cmp.Or(cfg.Name, cfg.Queue)
	cfg.Concurrency = max(cfg.Concurrency, 1)

	rmqOpts := []func(*rmq.ConsumerOptions){
		rmq.WithConsumerOptionsConsumerName(cfg.Name),
		rmq.WithConsumerOptionsQueueDurable,
		rmq.WithConsumerOptionsConcurrency(cfg.Concurrency),
	}
	if cfg.Exchange != "" {
		keys := cfg.RoutingKeys
		if len(keys) == 0 {
			keys = []string{"#"}
		}
		rmqOpts = append(
			rmqOpts,
			rmq.WithConsumerOptionsExchangeName(cfg.Exchange),
			rmq.WithConsumerOptionsExchangeKind(cmp.Or(cfg.ExchangeKind, "topic")),
			rmq.WithConsumerOptionsExchangeDeclare,
			rmq.WithConsumerOptionsExchangeDurable,
		)
		for _, k := range keys {
			rmqOpts = append(rmqOpts, rmq.WithConsumerOptionsRoutingKey(k))
		}
	}

	rc, err := rmq.NewConsumer(conn.conn, cfg.Queue, rmqOpts...)
	if err != nil {
		return nil, fmt.Errorf("rabbitmq: new consumer: %w", err)
	}
	h = consume.Instrument(h, consume.InstrumentConfig{
		Consumer:       cfg.Name,
		System:         "rabbitmq",
		Metrics:        cfg.Metrics,
		TracerProvider: cfg.TracerProvider,
	})
	c := &Consumer{log: log, cfg: cfg, env: broker.CloudEvents{}, handler: h, rc: rc}
	for _, opt := range opts {
		opt.applyConsumer(c)
	}
	return c, nil
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

// dispatch unpacks one delivery into a broker.Message and runs the
// instrumented handler under panic recovery, mapping the broker.Action back to
// an rmq.Action. A delivery the envelope rejects is discarded (nack without
// requeue): requeuing it would loop forever, and a dead-letter exchange of the
// queue receives it.
func (c *Consumer) dispatch(ctx context.Context, d rmq.Delivery) rmq.Action {
	action := consume.Process(ctx, c.log, c.cfg.Name, c.env, consume.Delivery{
		Body:    d.Body,
		Headers: headers(d.Headers, d.ContentType, d.RoutingKey),
		Topic:   d.Exchange,
	}, c.handler)
	return toAction(action)
}

// toAction maps a broker.Action onto the go-rabbitmq delivery action.
func toAction(a broker.Action) rmq.Action {
	switch a {
	case broker.Requeue:
		return rmq.NackRequeue
	case broker.Discard:
		return rmq.NackDiscard
	default:
		return rmq.Ack
	}
}

// headers projects the header table, the content_type property and the routing
// key of an AMQP delivery to the headers the envelope reads and
// broker.Message.Headers, dropping non-string values (envelope and trace
// headers are strings), so the consumer span continues the trace the message
// was created in.
func headers(t amqp.Table, contentType, routingKey string) map[string]string {
	if len(t) == 0 && contentType == "" && routingKey == "" {
		return nil
	}
	out := make(map[string]string, len(t)+2)
	for k, v := range t {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	if contentType != "" {
		out[contentTypeHeader] = contentType
	}
	if routingKey != "" {
		out[RoutingKeyHeader] = routingKey
	}
	return out
}
