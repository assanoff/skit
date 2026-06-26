// Package broker abstracts an event broker behind transport-agnostic Publisher
// and Consumer contracts.
//
// Messages travel the wire as CloudEvents v1.0 structured-mode JSON; this
// package owns that envelope so every transport (e.g. broker/rabbitmq) speaks
// the same shape and consumers can dedupe on the CloudEvents id. The interfaces
// here have no transport dependencies, so the same producer/consumer code can
// ship over RabbitMQ, Kafka, NATS, or a WebSocket gateway by swapping the
// adapter behind Publisher.
//
// # The abstraction
//
// Message carries CloudEvents context attributes (ID/Type/Source/Subject/Time/
// DataContentType) plus the business payload (Data) and transport-neutral
// routing: Topic (the logical destination) and an optional Key (routing/
// ordering key within the topic). Each transport maps Topic/Key to its own
// concept — a RabbitMQ exchange + routing key, a Kafka topic + partition key,
// a NATS subject. Headers carry transport headers such as W3C trace context.
//
//   - Publisher delivers a Message; implementations provide at-least-once
//     semantics, so a nil error means the broker durably accepted it.
//   - Handler processes one received Message and returns an Action.
//   - Action is the consumer's verdict: Ack (drop), Requeue (redeliver), or
//     Discard (drop without requeue / dead-letter).
//
// # CloudEvents envelope
//
// MarshalCloudEvent serializes a Message into a CloudEvents v1.0 structured-mode
// JSON envelope (SpecVersion is stamped automatically, ContentTypeCloudEvents is
// the wire content type); the business payload lands under "data". Transport
// fields (Topic, Key, Headers) are NOT part of the envelope — the transport
// applies them at publish time. UnmarshalCloudEvent reverses it, leaving the
// transport fields zero for the adapter to fill from the delivery.
//
//	body, err := broker.MarshalCloudEvent(broker.Message{
//	    Type:            "widget.created",
//	    DataContentType: "application/json",
//	    Data:            []byte(`{"id":"w-1"}`),
//	    Topic:           "widgets",
//	})
//
//	m, err := broker.UnmarshalCloudEvent(body) // m.Topic/Key/Headers are zero
//
// # How a transport plugs in
//
// A concrete adapter implements Publisher (and, for consumers, worker.Runnable)
// in a sub-package. broker/rabbitmq is the reference adapter: its Publisher maps
// Message.Topic/Key to a RabbitMQ exchange/routing key and publishes the
// CloudEvents body with publisher confirms; its Consumer decodes deliveries back
// into Messages and dispatches them to a broker.Handler. Application and domain
// code depend only on broker.Publisher / broker.Handler, never on the adapter.
package broker
