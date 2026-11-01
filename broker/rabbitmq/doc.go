// Package rabbitmq implements the broker abstraction over RabbitMQ.
//
// It is the concrete adapter for the broker package, built on
// github.com/wagslane/go-rabbitmq (automatic reconnection and publisher confirms
// on top of amqp091). It provides a connection manager (Conn via Dial), a
// confirm-mode Publisher (broker.Publisher) that sends the CloudEvents body with
// persistent delivery, and a Consumer (worker.Runnable) that decodes deliveries
// back into broker.Messages and dispatches them to a broker.Handler, recording
// each as a span and in the metrics.
//
// A single Conn transparently reconnects and is shared by every publisher and
// consumer in the process.
//
// # Usage
//
// Wire a publisher and a consumer against one connection:
//
//	conn, err := rabbitmq.Dial(rabbitmq.Config{
//	    User: "guest", Password: "guest", Host: "localhost", Port: "5672",
//	})
//	if err != nil { /* ... */ }
//	defer conn.Close()
//
//	pub, err := rabbitmq.NewPublisher(conn, rabbitmq.PublisherConfig{
//	    Source:         "myapp",
//	    TracerProvider: tp,
//	})
//	if err != nil { /* ... */ }
//	defer pub.Close()
//	err = pub.Publish(ctx, broker.Message{
//	    Type:        "widget.created", // → routing key
//	    ContentType: "application/json",
//	    Data:        []byte(`{"id":"w-1"}`),
//	    Topic:       "widgets", // → exchange
//	})
//
//	cons, err := rabbitmq.NewConsumer(log, conn, rabbitmq.ConsumerConfig{
//	    Queue:          "widgets.worker",
//	    Exchange:       "widgets",
//	    RoutingKeys:    []string{"widget.created"},
//	    Metrics:        reg,
//	    TracerProvider: tp,
//	}, func(ctx context.Context, m broker.Message) broker.Action {
//	    // ... process m ...
//	    return broker.Ack
//	})
//	if err != nil { /* ... */ }
//	group.Add(cons) // a worker.Runnable; Start blocks until ctx is canceled
//
// # Routing key
//
// A message goes to the exchange Message.Topic under the routing key
// Message.Type, so a queue binds to the event types it consumes, as above.
// WithRoutingKey derives the key from the whole message instead — say, to
// publish into a queue of another system bound to a key of its own:
//
//	pub, err := rabbitmq.NewPublisher(conn, rabbitmq.PublisherConfig{Source: "myapp"},
//	    rabbitmq.WithRoutingKey(func(m broker.Message) string {
//	        return "legacy." + m.Type
//	    }))
//
// A key of "" fails the publish with an error that is not broker.Permanent: the
// function is wrong, not the message, so the queue of events retries the event
// until a fixed function ships. Publishes are not mandatory: the broker drops a
// message that no binding matches without an error, so the event types are
// part of the contract of the bindings.
//
// # Tracing
//
// Every publish is a span "send <exchange>" with messaging.system rabbitmq and
// the routing key. A message without a trace context in its headers gets the
// PRODUCER span as its creation context, written into the AMQP headers; a
// message that carries one — outbox.Handler sends the context the event was
// created in — is sent under a CLIENT child of it. The consumer records every
// message as a CONSUMER span "process <exchange>" in the trace the message was
// created in (see broker).
//
// # Config
//
//   - Config: connection settings (User/Password/Host/Port/VHost), or a full
//     URL that overrides the component fields.
//   - Publisher: NewPublisher(conn, PublisherConfig, opts...); runs in
//     confirm mode for at-least-once delivery. PublisherConfig fields: Source
//     (required; the CloudEvents source stamped on messages that carry none),
//     TracerProvider (nil → the global provider). Option: WithRoutingKey.
//   - Consumer: NewConsumer(log, conn, ConsumerConfig, handler). ConsumerConfig
//     fields: Queue (required), Exchange (declared as a durable topic
//     exchange), RoutingKeys (default ["#"] when Exchange is set), Concurrency
//     (default 1), Name (default Queue), Metrics (the skit_broker_* registry;
//     nil — off), TracerProvider (nil → the global provider). A malformed
//     envelope is discarded (nack without requeue, so a dead-letter exchange of
//     the queue receives it); a panicking handler is recovered and the delivery
//     is requeued.
package rabbitmq
