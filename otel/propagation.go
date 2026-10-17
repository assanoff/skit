package otel

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Propagator returns the propagator skit uses on every boundary: W3C
// TraceContext (traceparent, tracestate) and nothing else. Baggage is not
// propagated, so it never reaches third parties, outbox rows or the broker.
// otelhttp and otelgrpc get it explicitly; otel/setup also installs it as the
// global propagator for third-party libraries.
func Propagator() propagation.TextMapPropagator {
	return propagation.TraceContext{}
}

// InjectToResponse writes the trace context of the request's own span — the
// server span this process opened — into the response headers as W3C
// traceparent / tracestate, so a client or QA can find the trace by the
// response. It writes nothing when ctx carries no span of this process: a
// remote parent extracted from the request is the caller's span, and echoing it
// back would point at the wrong span. Call it before the handler writes the
// status line.
func InjectToResponse(ctx context.Context, h http.Header) {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() || sc.IsRemote() {
		return
	}
	Propagator().Inject(ctx, propagation.HeaderCarrier(h))
}

// Carrier returns the trace context carried by ctx as a string map. Store the
// map on a message (e.g. an outbox event's headers) so a consumer can restore
// it with ExtractFromCarrier and continue the trace. Returns nil when ctx
// carries no span context.
func Carrier(ctx context.Context) map[string]string {
	mc := propagation.MapCarrier{}
	Propagator().Inject(ctx, mc)
	if len(mc) == 0 {
		return nil
	}
	return mc
}

// ExtractFromCarrier returns a context seeded with the trace context held in a
// string map produced by Carrier — the inverse of Carrier, for consumers that
// receive trace headers out of band (e.g. from a broker message).
func ExtractFromCarrier(ctx context.Context, carrier map[string]string) context.Context {
	return Propagator().Extract(ctx, propagation.MapCarrier(carrier))
}
