// Package brokertrace records the send span of the broker adapters
// (broker/kafka, broker/rabbitmq) and reads the creation context of a message
// from its headers, which the consumer core (broker/internal/consume) links
// its span to.
//
// StartSend opens the span "send <destination>" of one message. A message
// without a trace context gets a PRODUCER span, which becomes its creation
// context and goes out in the headers; a message that carries one — the
// context outbox.Handler sends with an event — gets a CLIENT child of it, and
// its headers go out as they are:
//
//	ctx, span, headers := brokertrace.StartSend(ctx, tracer, m.Topic, m.Headers,
//	    semconv.MessagingSystemRabbitMQ, semconv.MessagingMessageID(m.ID))
//	m.Headers = headers
//	err := send(ctx, m)
//	brokertrace.End(span, err)
package brokertrace
