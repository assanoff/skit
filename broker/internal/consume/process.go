// Package consume is the consumer core of the broker adapters: it decodes a
// delivery, runs the handler under panic recovery and records the handling as
// a span and in the skit_broker_* metrics.
package consume

import (
	"context"
	"log/slog"

	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/safetick"
)

// Process turns one raw delivery into a handler verdict. It:
//
//   - decodes body as a CloudEvents v1.0 envelope; a malformed envelope is
//     logged at warn level and returns Discard: requeuing it would loop
//     forever, and a dead-letter destination keeps it for a look;
//   - applies route to fill the transport fields (Topic, Headers) onto the
//     decoded Message, when route is non-nil;
//   - runs h under panic recovery, returning Requeue on panic so the message is
//     retried elsewhere rather than lost.
//
// log may be nil. The caller maps the returned Action onto its transport's ack
// primitives.
func Process(ctx context.Context, log *slog.Logger, name string, body []byte, route func(*broker.Message), h broker.Handler) broker.Action {
	m, err := broker.UnmarshalCloudEvent(body)
	if err != nil {
		if log != nil {
			log.WarnContext(ctx, "broker: malformed message, discarding", "consumer", name, "err", err, "body_size", len(body))
		}
		return broker.Discard
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
