package otel

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Config configures tracing.
type Config struct {
	// ServiceName tags every span; also used to create the named Tracer.
	ServiceName string
	// Endpoint is the OTLP/gRPC collector endpoint, e.g. "localhost:4317".
	Endpoint string
	// Insecure disables TLS to the collector (typical for local/in-cluster).
	Insecure bool
	// Probability is the sampling ratio in [0,1] applied to non-excluded routes.
	Probability float64
	// ExcludedRoutes are never sampled (health/readiness probes, etc.).
	ExcludedRoutes map[string]struct{}
}

// InitTracing configures a global TracerProvider exporting via OTLP/gRPC and
// returns a Tracer plus a shutdown function. The shutdown flushes pending spans.
func InitTracing(ctx context.Context, cfg Config) (trace.Tracer, func(context.Context) error, error) {
	opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.Endpoint)}
	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("otel: create exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(newEndpointExcluder(cfg.ExcludedRoutes, cfg.Probability)),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Tracer(cfg.ServiceName), tp.Shutdown, nil
}

// InjectTracing stores the tracer in ctx and ensures a trace id is available
// (generating one when there is no active span), so downstream logging can pick
// it up via GetTraceID. Call it once per request, before handling.
func InjectTracing(ctx context.Context, tracer trace.Tracer) context.Context {
	ctx = setTracer(ctx, tracer)

	traceID := trace.SpanFromContext(ctx).SpanContext().TraceID().String()
	if traceID == defaultTraceID {
		traceID = uuid.NewString()
	}
	return setTraceID(ctx, traceID)
}

// AddSpan starts a child span on the tracer stored in ctx. If no tracer is
// present it returns ctx unchanged and a no-op span, so callers never need a
// nil check.
func AddSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	tracer := tracerFrom(ctx)
	if tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	ctx, span := tracer.Start(ctx, name)
	if len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	return ctx, span
}

// InjectToRequest writes the current trace context into an outgoing request's
// headers so the callee can continue the trace.
func InjectToRequest(ctx context.Context, r *http.Request) {
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(r.Header))
}

// ExtractFromRequest returns a context seeded with the trace context carried by
// an incoming request's headers.
func ExtractFromRequest(ctx context.Context, r *http.Request) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(r.Header))
}

// Carrier returns the W3C trace context (traceparent/tracestate) and baggage
// carried by ctx as a string map, using the configured propagator. It is the
// transport-agnostic counterpart to InjectToRequest: store the map on a
// message (e.g. an outbox event's headers) so a consumer can extract it with
// ExtractFromCarrier and continue the trace. Returns nil when ctx carries no
// propagatable context.
func Carrier(ctx context.Context) map[string]string {
	mc := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, mc)
	if len(mc) == 0 {
		return nil
	}
	return mc
}

// ExtractFromCarrier returns a context seeded with the trace context held in a
// string map produced by Carrier — the inverse of Carrier, for consumers that
// receive trace headers out of band (e.g. from a broker message).
func ExtractFromCarrier(ctx context.Context, carrier map[string]string) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(carrier))
}
