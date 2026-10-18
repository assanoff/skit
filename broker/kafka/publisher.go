package kafka

import (
	"context"
	"errors"
	"fmt"
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

// Publisher publishes broker.Messages to Kafka as CloudEvents v1.0
// structured-mode JSON. It writes with RequireAll acks, so Publish returns nil
// only after every in-sync replica has the record — at-least-once delivery.
type Publisher struct {
	w      *kgo.Writer
	source string
	tracer trace.Tracer
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

// NewPublisher builds a Kafka publisher on the brokers of cfg.
func NewPublisher(cfg Config, pcfg PublisherConfig) (*Publisher, error) {
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
	}
	return &Publisher{w: w, source: pcfg.Source, tracer: otel.Tracer(pcfg.TracerProvider, scope)}, nil
}

// Publish sends m to the Kafka topic m.Topic, keyed by m.Key, as a CloudEvents
// record and blocks until the brokers acknowledge it. The span "send <topic>"
// covers the write; without a trace context in m.Headers it is the creation
// context of the message and goes out in its headers.
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

	attrs := []attribute.KeyValue{semconv.MessagingSystemKafka, semconv.MessagingMessageID(m.ID)}
	if m.Key != "" {
		attrs = append(attrs, semconv.MessagingKafkaMessageKey(m.Key))
	}
	ctx, span, headers := brokertrace.StartSend(ctx, p.tracer, m.Topic, m.Headers, attrs...)
	m.Headers = headers
	err := p.send(ctx, m)
	brokertrace.End(span, err)
	return err
}

// send writes m as a CloudEvents record and waits for the acknowledgement.
func (p *Publisher) send(ctx context.Context, m broker.Message) error {
	body, err := broker.MarshalCloudEvent(m)
	if err != nil {
		return fmt.Errorf("kafka publish: %w", err)
	}

	headers := make([]kgo.Header, 0, len(m.Headers))
	for k, v := range m.Headers {
		headers = append(headers, kgo.Header{Key: k, Value: []byte(v)})
	}

	msg := kgo.Message{
		Topic:   m.Topic,
		Value:   body,
		Headers: headers,
		Time:    m.Time,
	}
	if m.Key != "" {
		msg.Key = []byte(m.Key)
	}

	if err := p.w.WriteMessages(ctx, msg); err != nil {
		return fmt.Errorf("kafka publish: %w", err)
	}
	return nil
}

// Close flushes and closes the underlying writer.
func (p *Publisher) Close() error {
	if err := p.w.Close(); err != nil {
		return fmt.Errorf("kafka: close publisher: %w", err)
	}
	return nil
}
