package otel

import "context"

// Suppress marks ctx as "do not trace anything started from here". Spans started
// with a suppressed context — and their children — are dropped by the sampler
// otel/setup installs: they are neither recorded nor exported, so the cost is
// one context value and one non-recording span per call.
//
// Intended for background tick loops (pollers, outbox relays, sweepers,
// cleaners). Their poll/lease/cleanup queries fire every few seconds, carry no
// parent span, and would each surface in the trace UI as a separate single-span
// trace — thousands a day of "SELECT … FOR UPDATE SKIP LOCKED returned nothing",
// crowding out the traces that describe real traffic.
//
// Suppression is inherited by every context derived from a suppressed one, so
// nothing below the tick is traced either. When a tick does find work worth
// tracing, lift it for that work with Unsuppress — see the "Suppressing
// background traces" section of the package doc.
//
// Deliberately explicit at the call site: a name-based rule in the sampler
// ("drop parentless postgres: … spans") looks equivalent but goes silently
// stale the moment a span is renamed, and it also erases the only traces
// one-shot CLI commands produce, since none of them opens a root span.
func Suppress(ctx context.Context) context.Context {
	return context.WithValue(ctx, suppressKey, true)
}

// Unsuppress lifts Suppress for ctx and the contexts derived from it. A tick
// calls it for each item worth tracing, together with the item's own trace
// context (ExtractFromCarrier) or trace.WithNewRoot. The sampler sees the
// parent context with all its values even for a new root, so the mark must be
// lifted rather than left behind.
func Unsuppress(ctx context.Context) context.Context {
	return context.WithValue(ctx, suppressKey, false)
}

// IsSuppressed reports whether ctx, or any context it was derived from, went
// through Suppress. A nil context is not suppressed.
func IsSuppressed(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(suppressKey).(bool)
	return v
}
