package kafka

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"
	"uuid"

	kgo "github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/broker/internal/brokertrace"
	"github.com/assanoff/skit/otel"
)

// scope names the tracer of this package.
const scope = "github.com/assanoff/skit/broker/kafka"

// Publisher publishes broker.Messages to Kafka packed by its envelope,
// CloudEvents v1.0 structured-mode JSON by default. It writes with RequireAll acks, so Publish returns nil
// only after every in-sync replica has the record — at-least-once delivery.
type Publisher struct {
	w            *kgo.Writer
	source       string
	partitionKey func(m broker.Message) (string, error)
	env          broker.Envelope
	tracer       trace.Tracer
}

// Compile-time check.
var _ broker.Publisher = (*Publisher)(nil)

// PublisherConfig configures a Publisher.
type PublisherConfig struct {
	// Source is the CloudEvents source stamped on messages that don't carry
	// their own (required).
	Source string
	// TracerProvider records the span of every publish; nil → the global
	// provider.
	TracerProvider trace.TracerProvider
}

// WithPartitionKey sets how the partition key of a message is derived; by
// default a record has no key. The records of one key keep their order within
// a partition. An error of fn fails the publish as it is: wrap it in
// broker.Permanent when the message itself has no key.
func WithPartitionKey(fn func(m broker.Message) (string, error)) PublisherOption {
	return publisherFunc(func(p *Publisher) {
		if fn != nil {
			p.partitionKey = fn
		}
	})
}

// noKey leaves a record without a key.
func noKey(broker.Message) (string, error) { return "", nil }

// NewPublisher builds a Kafka publisher on the brokers of cfg.
func NewPublisher(cfg Config, pcfg PublisherConfig, opts ...PublisherOption) (*Publisher, error) {
	if pcfg.Source == "" {
		return nil, errors.New("kafka: new publisher: source is required")
	}
	if len(cfg.Brokers) == 0 {
		return nil, errors.New("kafka: new publisher: brokers are required")
	}
	w := &kgo.Writer{
		Addr: kgo.TCP(cfg.Brokers...),
		// Hash the message key so same-key records keep partition order; keyless
		// records round-robin.
		Balancer:               &kgo.Hash{},
		RequiredAcks:           kgo.RequireAll,
		AllowAutoTopicCreation: true,
		// Publish writes one record and waits for it: the default 1s timer
		// would hold every record for a batch that never fills.
		BatchTimeout: time.Millisecond,
	}
	p := &Publisher{
		w:            w,
		source:       pcfg.Source,
		partitionKey: noKey,
		env:          broker.CloudEvents{},
		tracer:       otel.Tracer(pcfg.TracerProvider, scope),
	}
	for _, opt := range opts {
		opt.applyPublisher(p)
	}
	return p, nil
}

// Publish sends m to the Kafka topic m.Topic, under its partition key, as a
// record packed by the envelope and blocks until the brokers acknowledge it.
// The span "send <topic>" covers the write; without a trace context in m.Headers it is the creation
// context of the message and goes out in its headers. A record over the size
// limit fails with a broker.Permanent error.
func (p *Publisher) Publish(ctx context.Context, m broker.Message) error {
	if m.Topic == "" {
		return errors.New("kafka publish: topic is required")
	}

	// Fill CloudEvents defaults the producer left blank.
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	if m.Source == "" {
		m.Source = p.source
	}
	if m.Time.IsZero() {
		m.Time = time.Now().UTC()
	}
	key, err := p.partitionKey(m)
	if err != nil {
		return fmt.Errorf("kafka publish: partition key: %w", err)
	}

	attrs := []attribute.KeyValue{semconv.MessagingSystemKafka, semconv.MessagingMessageID(m.ID)}
	if key != "" {
		attrs = append(attrs, semconv.MessagingKafkaMessageKey(key))
	}
	ctx, span, headers := brokertrace.StartSend(ctx, p.tracer, m.Topic, m.Headers, attrs...)
	m.Headers = headers
	err = p.send(ctx, m, key)
	brokertrace.End(span, err)
	return err
}

// send packs m with the envelope, writes it as a record under key ("" for
// none) and waits for the acknowledgement. The headers of the envelope go over
// the headers of m.
func (p *Publisher) send(ctx context.Context, m broker.Message, key string) error {
	body, envHeaders, err := p.env.Marshal(m)
	if err != nil {
		return fmt.Errorf("kafka publish: %w", err)
	}
	wire := make(map[string]string, len(m.Headers)+len(envHeaders))
	maps.Copy(wire, m.Headers)
	maps.Copy(wire, envHeaders)
	headers := make([]kgo.Header, 0, len(wire))
	for k, v := range wire {
		headers = append(headers, kgo.Header{Key: k, Value: []byte(v)})
	}

	msg := kgo.Message{
		Topic:   m.Topic,
		Value:   body,
		Headers: headers,
		Time:    m.Time,
	}
	if key != "" {
		msg.Key = []byte(key)
	}

	if err := p.w.WriteMessages(ctx, msg); err != nil {
		if tooLarge(err) {
			err = broker.Permanent(err)
		}
		return fmt.Errorf("kafka publish: %w", err)
	}
	return nil
}

// tooLarge reports whether a write failed on the size of the record: over
// BatchBytes of the writer or over the limit of the brokers. WriteMessages
// returns the error of the brokers in WriteErrors, which does not unwrap.
func tooLarge(err error) bool {
	if werr, ok := errors.AsType[kgo.WriteErrors](err); ok {
		return slices.ContainsFunc(werr, func(e error) bool {
			return errors.Is(e, kgo.MessageSizeTooLarge)
		})
	}
	return errors.Is(err, kgo.MessageSizeTooLarge)
}

// Close flushes and closes the underlying writer.
func (p *Publisher) Close() error {
	if err := p.w.Close(); err != nil {
		return fmt.Errorf("kafka: close publisher: %w", err)
	}
	return nil
}
