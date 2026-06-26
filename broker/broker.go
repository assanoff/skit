package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

const (
	// SpecVersion is the CloudEvents spec version stamped on every envelope.
	SpecVersion = "1.0"
	// ContentTypeCloudEvents is the wire content type for the structured-mode
	// JSON envelope produced by MarshalCloudEvent.
	ContentTypeCloudEvents = "application/cloudevents+json"
)

// Message is a broker message in CloudEvents terms plus the transport routing
// needed to deliver it. Data is the business payload only — the CloudEvents
// envelope is built around it by MarshalCloudEvent.
type Message struct {
	// --- CloudEvents context attributes ---

	// ID is the unique event id; consumers dedupe on it. Defaults to a fresh
	// UUID at publish time when empty.
	ID string
	// Type is the CloudEvents type, e.g. "widget.created".
	Type string
	// Source identifies the producer; a publisher fills it from its configured
	// source when empty.
	Source string
	// Subject is the optional CloudEvents subject.
	Subject string
	// Time is the event timestamp; defaults to now at publish time when zero.
	Time time.Time
	// DataContentType is the MIME type of Data, e.g. "application/json".
	DataContentType string
	// Data is the business payload (NOT the envelope).
	Data []byte

	// --- transport routing (transport-neutral) ---

	// Topic is the logical destination, mapped by each transport to its own
	// concept: a RabbitMQ exchange, a Kafka/NATS topic, a WebSocket channel.
	Topic string
	// Key is an optional routing/ordering key within the topic: a RabbitMQ
	// routing key, a Kafka partition key, a NATS subject suffix. May be empty
	// for transports that don't use one.
	Key string
	// Headers are transport headers (e.g. W3C trace context).
	Headers map[string]string
}

// Publisher publishes messages to the broker.
type Publisher interface {
	// Publish delivers m. Implementations should provide at-least-once
	// semantics (e.g. AMQP publisher confirms) so callers can treat a nil error
	// as durably accepted by the broker.
	Publish(ctx context.Context, m Message) error
	// Close releases publisher resources.
	Close() error
}

// Action is a consumer's verdict on a delivery, returned by a Handler.
type Action int

const (
	// Ack acknowledges successful processing; the broker drops the message.
	Ack Action = iota
	// Requeue negatively acknowledges and asks the broker to redeliver later.
	Requeue
	// Discard negatively acknowledges without requeue (drop / dead-letter).
	Discard
)

// Handler processes one received message and returns the broker Action. It must
// be safe for concurrent use when the consumer runs with concurrency > 1.
type Handler func(ctx context.Context, m Message) Action

// cloudEvent is the CloudEvents v1.0 structured-mode JSON envelope.
// See https://github.com/cloudevents/spec/blob/v1.0/spec.md#context-attributes
type cloudEvent struct {
	SpecVersion     string          `json:"specversion"`
	ID              string          `json:"id"`
	Type            string          `json:"type"`
	Source          string          `json:"source"`
	Subject         string          `json:"subject,omitempty"`
	Time            time.Time       `json:"time"`
	DataContentType string          `json:"datacontenttype,omitempty"`
	Data            json.RawMessage `json:"data"`
}

// MarshalCloudEvent serializes m as a CloudEvents v1.0 structured-mode JSON
// envelope: context attributes (id/type/source/...) live at the top level and
// the business payload goes under "data". Transport fields (Topic, Key,
// Headers) are NOT part of the envelope — they are applied by the transport at
// publish time.
func MarshalCloudEvent(m Message) ([]byte, error) {
	if m.Type == "" {
		return nil, fmt.Errorf("broker: marshal cloudevent: type is required")
	}
	data := m.Data
	if len(data) == 0 {
		data = []byte("null")
	}
	return json.Marshal(cloudEvent{
		SpecVersion:     SpecVersion,
		ID:              m.ID,
		Type:            m.Type,
		Source:          m.Source,
		Subject:         m.Subject,
		Time:            m.Time,
		DataContentType: m.DataContentType,
		Data:            data,
	})
}

// UnmarshalCloudEvent parses a structured-mode JSON envelope back into a
// Message. Transport fields are left zero; the caller fills Headers/Exchange/
// RoutingKey from the delivery if needed.
func UnmarshalCloudEvent(body []byte) (Message, error) {
	var ce cloudEvent
	if err := json.Unmarshal(body, &ce); err != nil {
		return Message{}, fmt.Errorf("broker: unmarshal cloudevent: %w", err)
	}
	if ce.SpecVersion == "" || ce.ID == "" || ce.Type == "" {
		return Message{}, fmt.Errorf("broker: unmarshal cloudevent: missing required envelope fields")
	}
	return Message{
		ID:              ce.ID,
		Type:            ce.Type,
		Source:          ce.Source,
		Subject:         ce.Subject,
		Time:            ce.Time,
		DataContentType: ce.DataContentType,
		Data:            ce.Data,
	}, nil
}
