package broker

import (
	"context"
	"time"
)

// Message is a broker message in CloudEvents terms plus the topic to deliver it
// to. Data is the business payload only — the Envelope of the adapter packs it
// for the wire. A transport derives any key of its own (a RabbitMQ routing key,
// a Kafka partition key) from the whole message.
type Message struct {
	// ID is the unique event id; consumers dedupe on it. Defaults to a fresh
	// UUID at publish time when empty.
	ID string
	// Type is the CloudEvents type, e.g. "widget.created".
	Type string
	// Source identifies the producer; a publisher fills it from its configured
	// source when empty.
	Source string
	// Time is the event timestamp; defaults to now at publish time when zero.
	Time time.Time
	// ContentType is the MIME type of Data, e.g. "application/json"; the
	// CloudEvents envelope carries data of a non-JSON type base64-encoded, and a
	// consumer decodes Data by it.
	ContentType string
	// Data is the business payload (NOT the envelope).
	Data []byte
	// Topic is the logical destination, mapped by each transport to its own
	// concept: a RabbitMQ exchange, a Kafka topic.
	Topic string
	// Headers are transport headers (e.g. W3C trace context). A publisher sends
	// the headers of its Envelope over them; a consumer sets every header of the
	// delivery.
	Headers map[string]string
}

// Publisher publishes messages to the broker.
type Publisher interface {
	// Publish delivers m. Implementations should provide at-least-once
	// semantics (e.g. AMQP publisher confirms) so callers can treat a nil error
	// as durably accepted by the broker.
	Publish(ctx context.Context, m Message) error
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
