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
// Message carries CloudEvents context attributes (ID/Type/Source/Time/
// ContentType) plus the business payload (Data) and the logical destination
// (Topic), which each transport maps to its own concept — a RabbitMQ exchange,
// a Kafka topic. A key within the topic is the transport's concern: it derives
// one from the whole message by a function the client gives the publisher
// (rabbitmq.WithRoutingKey, kafka.WithPartitionKey). Headers carry transport
// headers such as W3C trace context.
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
// the wire content type); the business payload lands under "data", or base64
// under "data_base64" when ContentType is not JSON. Transport fields (Topic,
// Headers) are NOT part of the envelope — the transport applies them at
// publish time. UnmarshalCloudEvent reverses it, leaving the
// transport fields zero for the adapter to fill from the delivery.
//
//	body, err := broker.MarshalCloudEvent(broker.Message{
//	    Type:        "widget.created",
//	    ContentType: "application/json",
//	    Data:        []byte(`{"id":"w-1"}`),
//	    Topic:       "widgets",
//	})
//
//	m, err := broker.UnmarshalCloudEvent(body) // m.Topic/Headers are zero
//
// # How a transport plugs in
//
// A concrete adapter implements Publisher (and, for consumers, worker.Runnable)
// in a sub-package. broker/rabbitmq is the reference adapter: its Publisher
// sends to the exchange Message.Topic under the routing key Message.Type and
// publishes the CloudEvents body with publisher confirms; its Consumer decodes
// deliveries back into Messages and dispatches them, instrumented, to a
// broker.Handler.
// Application and domain code depend only on broker.Publisher /
// broker.Handler, never on the adapter.
//
// # Metrics
//
// The consumers of the adapters record skit_broker_consumed_total by consumer
// and action (ack, requeue, discard, panic) and
// skit_broker_handle_duration_seconds by consumer on the registry of their
// config, under the name of the consumer:
//
//	cons, err := rabbitmq.NewConsumer(log, conn, rabbitmq.ConsumerConfig{
//	    Queue:          "orders.payments",
//	    Exchange:       "payments",
//	    Metrics:        reg,
//	    TracerProvider: tp,
//	}, app.HandlePaid)
//
// A consume loop of your own, like the generated consumers, wraps its handler
// in Instrument before passing it to Process, so the same metrics come from
// every transport.
//
// Publishing is measured by the queue of events (skit_queue_* by kind).
//
// # Tracing
//
// The adapters' publishers record every send as a span "send <destination>".
// A message without a trace context in its headers gets the PRODUCER span as
// its creation context, written into its headers; a message that carries one —
// outbox.Handler sends the context the event was created in — is sent under a
// CLIENT child of it.
//
// The consumers of the adapters, and Instrument in a consume loop of your own,
// record the handling of every message as a CONSUMER span
// "process <topic>". The creation context from the headers is both the parent
// and the link of that span. The OpenTelemetry messaging conventions ask for
// the link and allow the parent for a single message, as this package
// documents here: a request, the event it created and the handling of that
// event are one trace under one sampling decision. A handler that processes a
// batch would link every message's context instead. Requeue, Discard and a
// panic mark the span failed, with error.type set to the verdict. The adapters'
// consumers fill Message.Headers from the delivery; a consume loop of your own,
// like the generated consumers, sets them with rabbitmq.Headers or
// kafka.Headers, or the span starts a trace of its own.
package broker
