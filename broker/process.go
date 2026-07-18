package broker

import (
	"context"
	"log/slog"

	"github.com/assanoff/skit/safetick"
)

// Process turns one raw delivery into a handler verdict — the reusable core of
// any consumer loop, so a hand-written or scaffolded loop over a raw client
// (AMQP, Kafka, NATS) never re-implements decode / panic-recovery / verdict
// logic. It:
//
//   - decodes body as a CloudEvents v1.0 envelope; a malformed envelope is
//     logged and returns Ack (drop — requeuing it would loop forever);
//   - applies route to fill the transport fields (Topic/Key/Headers) onto the
//     decoded Message, when route is non-nil;
//   - runs h under panic recovery, returning Requeue on panic so the message is
//     retried elsewhere rather than lost.
//
// log may be nil. The caller maps the returned Action onto its transport's ack
// primitives (see rabbitmq.ToAction).
func Process(ctx context.Context, log *slog.Logger, name string, body []byte, route func(*Message), h Handler) Action {
	m, err := UnmarshalCloudEvent(body)
	if err != nil {
		if log != nil {
			log.Warn("broker: malformed message, dropping", "consumer", name, "err", err, "body_size", len(body))
		}
		return Ack
	}
	if route != nil {
		route(&m)
	}
	return Guard(ctx, log, name, m, h)
}

// Guard runs h(ctx, m) under panic recovery, returning Requeue if it panics so
// the message is retried rather than lost. Use it when the message is already
// decoded — e.g. a retry loop that decodes once and re-runs the handler; Process
// is decode + Guard for the common single-shot path. log may be nil.
func Guard(ctx context.Context, log *slog.Logger, name string, m Message, h Handler) Action {
	action := Requeue
	recovered := safetick.Guard(log, name, nil, func() {
		action = h(ctx, m)
	})
	if recovered && log != nil {
		log.Error("broker: handler panicked, requeueing", "consumer", name, "message_id", m.ID)
	}
	return action
}
