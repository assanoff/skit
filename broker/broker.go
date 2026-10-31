package broker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"strings"
	"time"
)

const (
	// SpecVersion is the CloudEvents spec version stamped on every envelope.
	SpecVersion = "1.0"
	// ContentTypeCloudEvents is the wire content type for the structured-mode
	// JSON envelope produced by MarshalCloudEvent.
	ContentTypeCloudEvents = "application/cloudevents+json"
)

// Message is a broker message in CloudEvents terms plus the topic to deliver it
// to. Data is the business payload only — the CloudEvents envelope is built
// around it by MarshalCloudEvent. A transport derives any key of its own (a
// RabbitMQ routing key, a Kafka partition key) from the whole message.
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
	// envelope carries data of a non-JSON type base64-encoded, and a consumer
	// decodes Data by it.
	ContentType string
	// Data is the business payload (NOT the envelope).
	Data []byte
	// Topic is the logical destination, mapped by each transport to its own
	// concept: a RabbitMQ exchange, a Kafka topic.
	Topic string
	// Headers are transport headers (e.g. W3C trace context).
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

// cloudEvent is the CloudEvents v1.0 structured-mode JSON envelope.
// See https://github.com/cloudevents/spec/blob/v1.0/spec.md#context-attributes
type cloudEvent struct {
	SpecVersion     string          `json:"specversion"`
	ID              string          `json:"id"`
	Type            string          `json:"type"`
	Source          string          `json:"source"`
	Time            time.Time       `json:"time"`
	DataContentType string          `json:"datacontenttype,omitempty"`
	Data            json.RawMessage `json:"data,omitempty"`
	DataBase64      []byte          `json:"data_base64,omitempty"`
}

// MarshalCloudEvent serializes m as a CloudEvents v1.0 structured-mode JSON
// envelope: context attributes (id/type/source/...) live at the top level and
// the business payload goes under "data" when ContentType is JSON (empty,
// application/json, */*+json) and base64 under "data_base64" otherwise.
// Transport fields (Topic, Headers) are NOT part of the envelope — they are
// applied by the transport at publish time. Its errors are Permanent: they
// concern the message itself.
func MarshalCloudEvent(m Message) ([]byte, error) {
	if m.Type == "" {
		return nil, Permanent(errors.New("broker: marshal cloudevent: type is required"))
	}
	ce := cloudEvent{
		SpecVersion:     SpecVersion,
		ID:              m.ID,
		Type:            m.Type,
		Source:          m.Source,
		Time:            m.Time,
		DataContentType: m.ContentType,
	}
	switch {
	case !jsonData(m.ContentType):
		ce.DataBase64 = m.Data
	case len(m.Data) == 0:
		ce.Data = json.RawMessage("null")
	default:
		ce.Data = m.Data
	}
	body, err := json.Marshal(ce)
	if err != nil {
		return nil, Permanent(fmt.Errorf("broker: marshal cloudevent: %w", err))
	}
	return body, nil
}

// jsonData reports whether data of content type ct is JSON, which the envelope
// carries as is: CloudEvents reads an absent type as application/json, and any
// */json or */*+json type is JSON.
func jsonData(ct string) bool {
	if ct == "" {
		return true
	}
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	return strings.HasSuffix(mt, "/json") || strings.HasSuffix(mt, "+json")
}

// UnmarshalCloudEvent parses a structured-mode JSON envelope back into a
// Message, taking Data from "data" or, decoded, from "data_base64". Transport
// fields are left zero; the caller fills Topic and Headers from the delivery.
func UnmarshalCloudEvent(body []byte) (Message, error) {
	var ce cloudEvent
	if err := json.Unmarshal(body, &ce); err != nil {
		return Message{}, fmt.Errorf("broker: unmarshal cloudevent: %w", err)
	}
	if ce.SpecVersion == "" || ce.ID == "" || ce.Type == "" {
		return Message{}, errors.New("broker: unmarshal cloudevent: missing required envelope fields")
	}
	data := []byte(ce.Data)
	if ce.DataBase64 != nil {
		data = ce.DataBase64
	}
	return Message{
		ID:          ce.ID,
		Type:        ce.Type,
		Source:      ce.Source,
		Time:        ce.Time,
		ContentType: ce.DataContentType,
		Data:        data,
	}, nil
}
