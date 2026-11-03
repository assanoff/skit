// Package broker abstracts an event broker behind transport-agnostic Publisher
// and Handler contracts.
//
// Messages travel the wire packed by an Envelope, CloudEvents v1.0
// structured-mode JSON by default, so every transport (e.g. broker/rabbitmq)
// speaks the same shape and consumers can dedupe on the CloudEvents id. The
// package has no transport dependencies, so the same producer and consumer
// code ships over RabbitMQ, Kafka or a transport of your own by swapping the
// adapter.
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
// # Envelope
//
// An Envelope packs a Message into the body and headers sent over the wire and
// unpacks a delivery back. The built-in one is CloudEvents: the CloudEvents
// v1.0 structured-mode JSON body, its context attributes at the top level and
// the business payload under "data", or base64 under "data_base64" when
// ContentType is not JSON, with the wire content type
// application/cloudevents+json. Transport fields (Topic, Headers) are not part
// of the body.
//
// Another format — CloudEvents binary mode, the envelope of an older system,
// the bare payload with the attributes in headers — is an Envelope of the
// client, set by WithEnvelope of the adapter on the publisher and on the
// consumers of one stream alike:
//
//	pub, err := kafka.NewPublisher(cfg, kafka.PublisherConfig{Source: "orders"},
//	    kafka.WithEnvelope(ceBinary{}))
//	cons, err := kafka.NewConsumer(log, cfg, kafka.ConsumerConfig{
//	    Topic: "orders", Group: "billing",
//	}, h, kafka.WithEnvelope(ceBinary{}))
//
// The adapter sends the headers of the envelope over Message.Headers and gives
// a consumer every header of the delivery as Message.Headers, so an envelope
// never drops the trace context; the content type travels in the header
// content-type. A delivery the envelope rejects is discarded with a warning.
//
// # How a transport plugs in
//
// A concrete adapter implements Publisher (and, for consumers, worker.Runnable)
// in a sub-package. broker/rabbitmq is the reference adapter: its Publisher
// sends to the exchange Message.Topic under the routing key Message.Type (by
// default; rabbitmq.WithRoutingKey) and publishes the CloudEvents body with
// publisher confirms; its Consumer decodes
// deliveries back into Messages and dispatches them, instrumented, to a
// broker.Handler, recovering a panic of the handler as Requeue. Application
// and domain code depend only on broker.Publisher / broker.Handler, never on
// the adapter.
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
// The consumers of the adapters record the handling of every message as a
// CONSUMER span "process <topic>". The creation context from the headers is
// both the parent and the link of that span. The OpenTelemetry messaging conventions ask for
// the link and allow the parent for a single message, as this package
// documents here: a request, the event it created and the handling of that
// event are one trace under one sampling decision. A handler that processes a
// batch would link every message's context instead. Requeue, Discard and a
// panic mark the span failed, with error.type set to the verdict. The
// consumers fill Message.Headers from the delivery, so a handler sees the
// headers the message was sent with.
package broker
