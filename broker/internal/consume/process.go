package consume

import (
	"context"
	"log/slog"

	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/safetick"
)

// Delivery is one message as the transport received it.
type Delivery struct {
	Body []byte
	// Headers are every header of the delivery, the wire content type under
	// content-type.
	Headers map[string]string
	// Topic is the destination the message was delivered from.
	Topic string
}

// Decode unpacks d with env into a Message whose Headers and Topic come from
// the delivery.
func Decode(env broker.Envelope, d Delivery) (broker.Message, error) {
	m, err := env.Unmarshal(d.Body, d.Headers)
	if err != nil {
		return broker.Message{}, err
	}
	m.Headers = d.Headers
	m.Topic = d.Topic
	return m, nil
}

// Process turns one delivery into a handler verdict. It decodes d with env; a
// delivery the envelope rejects is logged at warn level and returns Discard:
// requeuing it would loop forever, and a dead-letter destination keeps it for
// a look. It runs h under panic recovery, returning Requeue on panic so the
// message is retried elsewhere rather than lost.
//
// log may be nil. The caller maps the returned Action onto its transport's ack
// primitives.
func Process(ctx context.Context, log *slog.Logger, name string, env broker.Envelope, d Delivery, h broker.Handler) broker.Action {
	m, err := Decode(env, d)
	if err != nil {
		if log != nil {
			log.WarnContext(ctx, "broker: malformed message, discarding", "consumer", name, "err", err, "body_size", len(d.Body))
		}
		return broker.Discard
	}
	return Guard(ctx, log, name, m, h)
}

// Guard runs h(ctx, m) under panic recovery, returning Requeue if it panics so
// the message is retried rather than lost. Use it when the message is already
// decoded — e.g. a retry loop that decodes once and re-runs the handler; Process
// is decode + Guard for the common single-shot path. log may be nil.
func Guard(ctx context.Context, log *slog.Logger, name string, m broker.Message, h broker.Handler) broker.Action {
	action := broker.Requeue
	recovered := safetick.Guard(ctx, log, name, nil, func() {
		action = h(ctx, m)
	})
	if recovered && log != nil {
		log.ErrorContext(ctx, "broker: handler panicked, requeueing", "consumer", name, "message_id", m.ID)
	}
	return action
}
