package kafka

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	kgo "github.com/segmentio/kafka-go"

	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/logger"
)

// DefaultMaxRetries bounds how many times a broker.Requeue verdict retries the
// same record in place before it is committed (dropped) to unblock the
// partition. Add a dead-letter topic for real poison-message handling.
const DefaultMaxRetries = 5

// DefaultRetryBackoff is the wait between in-place retries of a Requeue'd record.
const DefaultRetryBackoff = time.Second

// ConsumerConfig configures a Consumer.
type ConsumerConfig struct {
	// Brokers is the bootstrap broker list. Required.
	Brokers []string
	// Topic to consume. Required.
	Topic string
	// GroupID is the consumer group; replicas sharing it split the partitions.
	// Required for at-least-once group consumption.
	GroupID string
	// Concurrency is the number of reader goroutines in the group (default 1).
	// Each reader is assigned a disjoint set of partitions.
	Concurrency int
	// Name identifies the consumer in the supervisor and logs (default "consumer").
	Name string
	// MaxRetries bounds in-place retries of a Requeue'd record (default
	// DefaultMaxRetries). RetryBackoff is the wait between them.
	MaxRetries   int
	RetryBackoff time.Duration
}

// Consumer consumes CloudEvents from Kafka and dispatches them to a
// broker.Handler. It implements worker.Runnable: Start blocks until the context
// is canceled (or Stop is called), then drains its readers.
type Consumer struct {
	log     *logger.Logger
	cfg     ConsumerConfig
	handler broker.Handler

	mu      sync.Mutex
	readers []*kgo.Reader
	once    sync.Once
}

// New builds a Consumer from a transport-neutral broker.Subscription, mapping it
// onto Kafka concepts (Topic → topic, Group → consumer group; Filters have no
// Kafka equivalent and are ignored). It is the pluggable entry point the
// rabbitmq adapter also offers, so wiring depends on broker.Subscription.
func New(cfg Config, log *logger.Logger, sub broker.Subscription, h broker.Handler) (*Consumer, error) {
	return NewConsumer(ConsumerConfig{
		Brokers:     cfg.Brokers,
		Topic:       sub.Topic,
		GroupID:     sub.Group,
		Concurrency: sub.Concurrency,
		Name:        sub.Name,
	}, log, h)
}

// NewConsumer builds a Consumer from the Kafka-specific config.
func NewConsumer(cfg ConsumerConfig, log *logger.Logger, h broker.Handler) (*Consumer, error) {
	if len(cfg.Brokers) == 0 {
		return nil, fmt.Errorf("kafka: new consumer: brokers are required")
	}
	if cfg.Topic == "" {
		return nil, fmt.Errorf("kafka: new consumer: topic is required")
	}
	if cfg.GroupID == "" {
		return nil, fmt.Errorf("kafka: new consumer: group id is required")
	}
	if cfg.Name == "" {
		cfg.Name = "consumer"
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 1
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = DefaultMaxRetries
	}
	if cfg.RetryBackoff <= 0 {
		cfg.RetryBackoff = DefaultRetryBackoff
	}
	return &Consumer{log: log, cfg: cfg, handler: h}, nil
}

// Name identifies the consumer in the supervisor and logs.
func (c *Consumer) Name() string { return c.cfg.Name }

// Start runs Concurrency readers in the group until ctx is canceled (or Stop is
// called). It blocks, mirroring the other Runnable transports.
func (c *Consumer) Start(ctx context.Context) error {
	var wg sync.WaitGroup
	for i := 0; i < c.cfg.Concurrency; i++ {
		r := kgo.NewReader(kgo.ReaderConfig{
			Brokers: c.cfg.Brokers,
			GroupID: c.cfg.GroupID,
			Topic:   c.cfg.Topic,
		})
		c.mu.Lock()
		c.readers = append(c.readers, r)
		c.mu.Unlock()

		wg.Add(1)
		go func(r *kgo.Reader) {
			defer wg.Done()
			c.consume(ctx, r)
		}(r)
	}

	c.log.Info(ctx, "kafka consumer started",
		"topic", c.cfg.Topic, "group", c.cfg.GroupID, "consumer", c.cfg.Name, "readers", c.cfg.Concurrency)

	<-ctx.Done()
	c.close()
	wg.Wait()
	return nil
}

// Stop closes the readers, unblocking Start.
func (c *Consumer) Stop(context.Context) error {
	c.close()
	return nil
}

func (c *Consumer) close() {
	c.once.Do(func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		for _, r := range c.readers {
			_ = r.Close()
		}
	})
}

// consume leases records one at a time (FetchMessage does not auto-commit),
// runs the handler, and commits the offset only on a terminal verdict — the
// at-least-once loop. It returns when ctx is canceled or the reader is closed.
func (c *Consumer) consume(ctx context.Context, r *kgo.Reader) {
	for {
		msg, err := r.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				return // graceful shutdown
			}
			c.log.Error(ctx, "kafka consumer: fetch", "consumer", c.cfg.Name, "err", err)
			continue
		}
		c.handle(ctx, r, msg)
	}
}

// handle decodes one record and drives the handler, committing the offset once
// the record reaches a terminal state (Ack, Discard, or a Requeue that exhausts
// its retries). A malformed envelope is committed (dropped) — retrying it would
// loop forever.
func (c *Consumer) handle(ctx context.Context, r *kgo.Reader, msg kgo.Message) {
	m, err := broker.UnmarshalCloudEvent(msg.Value)
	if err != nil {
		c.log.Warn(ctx, "kafka consumer: malformed message, dropping",
			"consumer", c.cfg.Name, "err", err, "offset", msg.Offset)
		c.commit(ctx, r, msg)
		return
	}
	m.Topic = msg.Topic
	m.Key = string(msg.Key)
	m.Headers = headers(msg.Headers)

	for attempt := 0; ; attempt++ {
		switch c.dispatch(ctx, m) {
		case broker.Requeue:
			if attempt >= c.cfg.MaxRetries {
				c.log.Error(ctx, "kafka consumer: retries exhausted, dropping (add a dead-letter topic)",
					"consumer", c.cfg.Name, "message_id", m.ID, "offset", msg.Offset)
				c.commit(ctx, r, msg)
				return
			}
			select {
			case <-ctx.Done():
				return // shutdown: leave uncommitted so it is redelivered
			case <-time.After(c.cfg.RetryBackoff):
			}
		default: // Ack or Discard: terminal
			c.commit(ctx, r, msg)
			return
		}
	}
}

// dispatch runs the handler under panic recovery (via broker.Guard), defaulting
// to Requeue on panic so the record is retried rather than silently dropped.
func (c *Consumer) dispatch(ctx context.Context, m broker.Message) broker.Action {
	return broker.Guard(ctx, c.log.Slog(), c.cfg.Name, m, c.handler)
}

func (c *Consumer) commit(ctx context.Context, r *kgo.Reader, msg kgo.Message) {
	if err := r.CommitMessages(ctx, msg); err != nil && ctx.Err() == nil {
		c.log.Error(ctx, "kafka consumer: commit", "consumer", c.cfg.Name, "offset", msg.Offset, "err", err)
	}
}

func headers(hs []kgo.Header) map[string]string {
	if len(hs) == 0 {
		return nil
	}
	out := make(map[string]string, len(hs))
	for _, h := range hs {
		out[h.Key] = string(h.Value)
	}
	return out
}
