// Package otel bootstraps OpenTelemetry tracing and provides helpers to inject
// a trace id into the context (and therefore into logs), open child spans, and
// propagate trace context across service boundaries.
//
// InitTracing wires a global TracerProvider that exports spans over OTLP/gRPC
// and samples with a parent-based ratio sampler that always drops excluded
// routes (health/readiness probes) and anything started from a suppressed
// context (see Suppress). InjectTracing seeds the request context
// with the tracer and a guaranteed trace id, so GetTraceID — usable directly as
// logger.TraceIDFn — makes every log line carry the active trace.
//
// # Startup
//
// Initialize once and defer the returned shutdown to flush pending spans:
//
//	tracer, shutdown, err := otel.InitTracing(ctx, otel.Config{
//		ServiceName:    "myapp",
//		Endpoint:       "localhost:4317",
//		Insecure:       true,
//		Probability:    0.1,
//		ExcludedRoutes: map[string]struct{}{"/healthz": {}, "/readyz": {}},
//	})
//	if err != nil {
//		return err
//	}
//	defer shutdown(context.Background())
//
//	log := logger.New(os.Stdout, logger.Config{
//		Service: "myapp", TraceIDFn: otel.GetTraceID, // every line gets trace_id
//	})
//
// # Per request
//
// Call InjectTracing once before handling so a trace id is always present, then
// open child spans on the tracer carried by the context:
//
//	ctx = otel.InjectTracing(r.Context(), tracer)
//	ctx, span := otel.AddSpan(ctx, "load-widget", attribute.String("id", id))
//	defer span.End()
//
// AddSpan returns a no-op span when no tracer is in ctx, so callers never need a
// nil check — and ending it never ends the span already in ctx.
//
// The server span of a request is opened by otelhttp (middleware.TraceRequest
// continues it); InjectToResponse returns its traceparent so a client can find
// the trace by the response.
//
// # Suppressing background traces
//
// Background tick loops — pollers, outbox relays, sweepers, cleaners — run every
// few seconds with no request behind them, so each poll query exports its own
// single-span trace and buries the traces that describe real traffic. Wrap the
// tick context in Suppress and the sampler drops those spans outright:
//
//	ctx = otel.Suppress(ctx)                 // once per tick
//	rows, err := store.LeasePending(ctx)     // no span exported
//
// Suppression is inherited by every derived context, so nothing below the tick is
// traced either. When a tick finds work worth tracing, start that span from
// context.Background() instead of from the suppressed context. Re-inject the
// tracer, since it travels in the context too:
//
//	rowCtx := otel.InjectTracing(context.Background(), tracer)
//	rowCtx, span := otel.AddSpan(rowCtx, "dispatch-row")
//
// AddSpan takes no span links; to associate the new root with the tick, start it
// on the tracer directly with trace.WithLinks(trace.LinkFromContext(ctx)).
//
// Note that re-rooting also detaches cancellation: derive a fresh
// context.WithTimeout (or check the tick's ctx.Err() between rows) so shutdown
// still stops the loop.
//
// # Propagation
//
// Carry trace context across boundaries with the W3C propagator. For HTTP, use
// InjectToRequest on the caller and ExtractFromRequest on the callee. For
// out-of-band transports (e.g. an outbox event's headers), Carrier serializes
// the context to a string map and ExtractFromCarrier restores it:
//
//	otel.InjectToRequest(ctx, req)              // outgoing HTTP
//	ctx = otel.ExtractFromRequest(ctx, r)       // incoming HTTP
//
//	headers := otel.Carrier(ctx)                // store on a message
//	ctx = otel.ExtractFromCarrier(ctx, headers) // restore in a consumer
//
// # Config
//
// Config fields for InitTracing:
//   - ServiceName: tags every span and names the returned Tracer.
//   - Endpoint: OTLP/gRPC collector endpoint, e.g. "localhost:4317".
//   - Insecure: disable TLS to the collector (local/in-cluster).
//   - Probability: sampling ratio in [0,1] for non-excluded routes.
//   - ExcludedRoutes: route names that are never sampled (probes, etc.); the
//     route is read from the span name set by HTTP middleware.
package otel
