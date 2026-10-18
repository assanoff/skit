// Package rabbitmq implements the broker abstraction over RabbitMQ.
//
// It is the concrete adapter for the broker package, built on
// github.com/wagslane/go-rabbitmq (automatic reconnection and publisher confirms
// on top of amqp091). It provides a connection manager (Conn via Dial), a
// confirm-mode Publisher (broker.Publisher) that sends the CloudEvents body with
// persistent delivery, and a Consumer (worker.Runnable) that decodes deliveries
// back into broker.Messages and dispatches them to a broker.Handler.
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
//	    Type:            "widget.created",
//	    DataContentType: "application/json",
//	    Data:            []byte(`{"id":"w-1"}`),
//	    Topic:           "widgets", // → exchange; Key → routing key
//	})
//
//	h := broker.Instrument(func(ctx context.Context, m broker.Message) broker.Action {
//	    // ... process m ...
//	    return broker.Ack
//	}, broker.InstrumentConfig{Consumer: "widgets", System: "rabbitmq", TracerProvider: tp})
//	cons, err := rabbitmq.NewConsumer(conn, log, rabbitmq.ConsumerConfig{
//	    Queue:    "widgets.worker",
//	    Exchange: "widgets",
//	}, h)
//	if err != nil { /* ... */ }
//	group.Add(cons) // a worker.Runnable; Start blocks until ctx is canceled
//
// # Topic mapping
//
// By default Message.Topic maps to the exchange and Message.Key to the routing
// key, falling back to the topic as the routing key when no key is set (so a
// single-key topic still routes with no configuration). Override the mapping
// with WithTopicMapper when topic naming does not map 1:1 to exchanges:
//
//	pub, err := rabbitmq.NewPublisher(conn, rabbitmq.PublisherConfig{Source: "myapp"},
//	    rabbitmq.WithTopicMapper(func(topic, key string) (exchange, rk string) {
//	        return "events", topic + "." + key
//	    }))
//
// # Tracing
//
// Every publish is a span "send <exchange>" with messaging.system rabbitmq and
// the routing key. A message without a trace context in its headers gets the
// PRODUCER span as its creation context, written into the AMQP headers; a
// message that carries one — the outbox relay sends the context the event was
// created in — is sent under a CLIENT child of it. The consumer span comes
// from broker.Instrument around the handler.
//
// # Config
//
//   - Config: connection settings (User/Password/Host/Port/VHost), or a full
//     URL that overrides the component fields.
//   - Publisher: NewPublisher(conn, PublisherConfig, opts...); runs in
//     confirm mode for at-least-once delivery. PublisherConfig fields: Source
//     (required; the CloudEvents source stamped on messages that carry none),
//     TracerProvider (nil → the global provider). Option: WithTopicMapper.
//   - Consumer: NewConsumer(conn, log, ConsumerConfig, handler). ConsumerConfig
//     fields: Queue (required), Exchange, ExchangeKind (default "topic"),
//     RoutingKeys (default ["#"] when Exchange is set), Concurrency (default 1),
//     Name (default "consumer"). A malformed envelope is Ack'd (dropped); a
//     panicking handler is recovered and the delivery is requeued.
package rabbitmq
