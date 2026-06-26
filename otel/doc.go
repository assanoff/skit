// Package otel bootstraps OpenTelemetry tracing and provides helpers to inject
// a trace id into the context (and therefore into logs), open child spans, and
// propagate trace context across service boundaries.
//
// InitTracing wires a global TracerProvider that exports spans over OTLP/gRPC
// and samples with a parent-based ratio sampler that always drops excluded
// routes (health/readiness probes). InjectTracing seeds the request context
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
// nil check.
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
