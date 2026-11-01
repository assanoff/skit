package kafka

import (
	"cmp"
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	kgo "github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel/trace"

	"github.com/assanoff/skit/broker"
)

// maxRetries bounds how many times a broker.Requeue verdict retries the same
// record in place before it is committed (dropped) to unblock the partition.
// Add a dead-letter topic for real poison-message handling.
const maxRetries = 5

// retryBackoff is the wait between in-place retries of a requeued record.
const retryBackoff = time.Second

// ConsumerConfig configures a Consumer.
type ConsumerConfig struct {
	// Topic to consume. Required.
	Topic string
	// Group is the consumer group; replicas sharing it split the partitions.
	// Required.
	Group string
	// Concurrency is the number of reader goroutines in the group (default 1).
	// Each reader is assigned a disjoint set of partitions.
	Concurrency int
	// Name identifies the consumer in the supervisor, logs and metrics
	// (default Group).
	Name string
	// Metrics is the registry for the skit_broker_* consumer metrics; nil —
	// metrics off.
	Metrics prometheus.Registerer
	// TracerProvider records the consumer span of every message; nil → the
	// global provider.
	TracerProvider trace.TracerProvider
}

// Consumer consumes CloudEvents from Kafka and dispatches them to a
// broker.Handler. It implements worker.Runnable: Start blocks until the context
// is canceled (or Stop is called), then drains its readers.
type Consumer struct {
	log     *slog.Logger
	brokers []string
	cfg     ConsumerConfig
	handler broker.Handler

	mu      sync.Mutex
	readers []*kgo.Reader
	closed  bool
}

// NewConsumer builds a Consumer of h on the brokers of cfg. Every attempt of h
// at a record is a consumer span in the trace the message was created in and,
// with ccfg.Metrics, a record in skit_broker_*; a panic of h retries the record.
func NewConsumer(log *slog.Logger, cfg Config, ccfg ConsumerConfig, h broker.Handler) (*Consumer, error) {
	if len(cfg.Brokers) == 0 {
		return nil, errors.New("kafka: new consumer: brokers are required")
	}
	if ccfg.Topic == "" {
		return nil, errors.New("kafka: new consumer: topic is required")
	}
	if ccfg.Group == "" {
		return nil, errors.New("kafka: new consumer: group is required")
	}
	ccfg.Name = cmp.Or(ccfg.Name, ccfg.Group)
	ccfg.Concurrency = max(ccfg.Concurrency, 1)
	h = broker.Instrument(h, broker.InstrumentConfig{
		Consumer:       ccfg.Name,
		System:         "kafka",
		Metrics:        ccfg.Metrics,
		TracerProvider: ccfg.TracerProvider,
	})
	return &Consumer{log: log, brokers: cfg.Brokers, cfg: ccfg, handler: h}, nil
}

// Name identifies the consumer in the supervisor and logs.
func (c *Consumer) Name() string { return c.cfg.Name }

// Start runs Concurrency readers in the group until ctx is canceled (or Stop is
// called). It blocks, mirroring the other Runnable transports.
func (c *Consumer) Start(ctx context.Context) error {
	var wg sync.WaitGroup
	for range c.cfg.Concurrency {
		r := kgo.NewReader(kgo.ReaderConfig{
			Brokers: c.brokers,
			GroupID: c.cfg.Group,
			Topic:   c.cfg.Topic,
		})
		c.mu.Lock()
		if c.closed {
			// Stop / ctx-cancel raced ahead of reader creation; close this one
			// now so it can't leak, and don't start a goroutine for it.
			c.mu.Unlock()
			_ = r.Close()
			continue
		}
		c.readers = append(c.readers, r)
		c.mu.Unlock()

		wg.Add(1)
		go func(r *kgo.Reader) {
			defer wg.Done()
			c.consume(ctx, r)
		}(r)
	}

	c.log.InfoContext(ctx, "kafka consumer started",
		"topic", c.cfg.Topic, "group", c.cfg.Group, "consumer", c.cfg.Name, "readers", c.cfg.Concurrency)

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

// close closes every reader and marks the consumer closed. It is idempotent
// (kafka-go Reader.Close is safe to call twice) and, together with the closed
// check in Start, guarantees no reader is leaked when Stop races reader startup.
func (c *Consumer) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	for _, r := range c.readers {
		_ = r.Close()
	}
	c.readers = nil
}

// consume leases records one at a time (FetchMessage does not auto-commit),
// runs the handler, and commits the offset only on a terminal verdict — the
// at-least-once loop. It returns when ctx is canceled or the reader is closed.
func (c *Consumer) consume(ctx context.Context, r *kgo.Reader) {
	for {
		msg, err := r.FetchMessage(ctx)
		if err != nil {
			// Shutdown: ctx canceled, or Stop closed the reader (io.EOF).
			if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
				return
			}
			c.log.ErrorContext(ctx, "kafka consumer: fetch", "consumer", c.cfg.Name, "err", err)
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
		c.log.WarnContext(ctx, "kafka consumer: malformed message, dropping",
			"consumer", c.cfg.Name, "err", err, "offset", msg.Offset)
		c.commit(ctx, r, msg)
		return
	}
	m.Topic = msg.Topic
	m.Headers = Headers(msg.Headers)

	for attempt := 0; ; attempt++ {
		switch c.dispatch(ctx, m) {
		case broker.Requeue:
			if attempt >= maxRetries {
				c.log.ErrorContext(ctx, "kafka consumer: retries exhausted, dropping (add a dead-letter topic)",
					"consumer", c.cfg.Name, "message_id", m.ID, "offset", msg.Offset)
				c.commit(ctx, r, msg)
				return
			}
			select {
			case <-ctx.Done():
				return // shutdown: leave uncommitted so it is redelivered
			case <-time.After(retryBackoff):
			}
		default: // Ack or Discard: terminal
			c.commit(ctx, r, msg)
			return
		}
	}
}

// dispatch runs the instrumented handler under panic recovery (via
// broker.Guard), defaulting to Requeue on panic so the record is retried rather
// than silently dropped.
func (c *Consumer) dispatch(ctx context.Context, m broker.Message) broker.Action {
	return broker.Guard(ctx, c.log, c.cfg.Name, m, c.handler)
}

func (c *Consumer) commit(ctx context.Context, r *kgo.Reader, msg kgo.Message) {
	if err := r.CommitMessages(ctx, msg); err != nil && ctx.Err() == nil {
		c.log.ErrorContext(ctx, "kafka consumer: commit", "consumer", c.cfg.Name, "offset", msg.Offset, "err", err)
	}
}

// Headers maps the headers of a Kafka record to broker.Message.Headers. A
// consume loop of your own sets it on the message, so broker.Instrument
// continues the trace the message was created in.
func Headers(hs []kgo.Header) map[string]string {
	if len(hs) == 0 {
		return nil
	}
	out := make(map[string]string, len(hs))
	for _, h := range hs {
		out[h.Key] = string(h.Value)
	}
	return out
}
