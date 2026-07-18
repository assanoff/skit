package kafka

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	kgo "github.com/segmentio/kafka-go"

	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/logger"
)

// Publisher publishes broker.Messages to Kafka as CloudEvents v1.0
// structured-mode JSON. It writes with RequireAll acks, so Publish returns nil
// only after every in-sync replica has the record — at-least-once delivery.
type Publisher struct {
	log    *logger.Logger
	w      *kgo.Writer
	source string
}

// Compile-time check.
var _ broker.Publisher = (*Publisher)(nil)

// NewPublisher builds a Kafka publisher. source is the CloudEvents source
// stamped on messages that don't carry their own.
func NewPublisher(cfg Config, source string, log *logger.Logger) (*Publisher, error) {
	if source == "" {
		return nil, fmt.Errorf("kafka: new publisher: source is required")
	}
	if len(cfg.Brokers) == 0 {
		return nil, fmt.Errorf("kafka: new publisher: brokers are required")
	}
	w := &kgo.Writer{
		Addr: kgo.TCP(cfg.Brokers...),
		// Hash the message key so same-key records keep partition order; keyless
		// records round-robin.
		Balancer:               &kgo.Hash{},
		RequiredAcks:           kgo.RequireAll,
		AllowAutoTopicCreation: true,
	}
	return &Publisher{log: log, w: w, source: source}, nil
}

// Publish sends m to the Kafka topic m.Topic, keyed by m.Key, as a CloudEvents
// record and blocks until the brokers acknowledge it.
func (p *Publisher) Publish(ctx context.Context, m broker.Message) error {
	if m.Topic == "" {
		return fmt.Errorf("kafka publish: topic is required")
	}

	// Fill CloudEvents defaults the producer left blank.
	if m.ID == "" {
		m.ID = uuid.NewString()
	}
	if m.Source == "" {
		m.Source = p.source
	}
	if m.Time.IsZero() {
		m.Time = time.Now().UTC()
	}

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
	return p.w.Close()
}
