// Package otel is the OpenTelemetry API of skit: tracers for SDK packages,
// span errors, suppression of background traces and W3C trace context
// propagation. It imports only the OpenTelemetry API; the SDK, the Resource
// and the exporter are installed by otel/setup and otel/otlp.
//
// # Tracers
//
// A component takes a trace.TracerProvider in its Config and gets its tracer
// once, in the constructor. The scope is the package import path; nil means the
// global provider:
//
//	type Config struct {
//		TracerProvider trace.TracerProvider // nil → global
//	}
//
//	func New(cfg Config) *Store {
//		return &Store{tracer: otel.Tracer(cfg.TracerProvider, "example.com/app/store")}
//	}
//
// Spans are opened with tracer.Start. Neither the tracer nor the provider
// travels in the context: the context carries only the span context.
//
// # Errors
//
// SetError marks a span as failed with status Error and error.type, taken from
// an ErrorType() string method in the error chain:
//
//	ctx, span := s.tracer.Start(ctx, "load")
//	defer span.End()
//	if err := s.load(ctx); err != nil {
//		otel.SetError(span, err)
//		return err
//	}
//
// The details of the error belong to the log record with the "err" key; skit
// does not call span.RecordError.
//
// # Suppressing background traces
//
// Background tick loops — pollers, outbox relays, sweepers, cleaners — run every
// few seconds with no request behind them, so each poll query would export its
// own single-span trace and bury the traces that describe real traffic. Wrap the
// tick context in Suppress and the sampler drops those spans outright:
//
//	ctx = otel.Suppress(ctx)             // once per tick
//	rows, err := store.LeasePending(ctx) // no span exported
//
// Suppression is inherited by every derived context. When the tick finds work
// worth tracing, lift it for that work and continue the item's own trace:
//
//	itemCtx := otel.ExtractFromCarrier(otel.Unsuppress(ctx), row.Headers)
//	itemCtx, span := tracer.Start(itemCtx, "dispatch")
//
// # Propagation
//
// Propagator is W3C TraceContext only: baggage never leaves the process. For
// out-of-band transports (e.g. an outbox event's headers), Carrier serializes
// the trace context to a string map and ExtractFromCarrier restores it:
//
//	headers := otel.Carrier(ctx)                // store on a message
//	ctx = otel.ExtractFromCarrier(ctx, headers) // restore in a consumer
//
// InjectToResponse writes the traceparent of the request's server span into
// the response headers, so a client can find the trace by the response.
package otel
