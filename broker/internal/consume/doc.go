// Package consume is the consumer core of the broker adapters (broker/kafka,
// broker/rabbitmq): it decodes a delivery with the envelope of the consumer,
// runs the handler under panic recovery and records the handling as a span
// and in the skit_broker_* metrics. An adapter maps the returned
// broker.Action onto the acks of its transport.
//
//	h = consume.Instrument(h, consume.InstrumentConfig{...}) // once, at start
//
//	action := consume.Process(ctx, log, name, env, consume.Delivery{
//	    Body: d.Body, Headers: headers, Topic: topic,
//	}, h)
//
// A delivery the envelope rejects is logged and discarded: requeued, it would
// loop forever. A handler that panics gets Requeue, so the message is retried
// rather than lost. Guard is the recovery alone, for an adapter that decodes
// once and retries the handler itself.
package consume
