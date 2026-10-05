package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matryer/is"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/assanoff/skit/middleware"
	skotel "github.com/assanoff/skit/otel"
)

// incoming is a traceparent from a caller's trace, unrelated to any span the
// test starts.
const incoming = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"

// useTraceContext sets the global propagator TraceRequest extracts with, the way
// otel.InitTracing does, and restores the previous one after the test.
func useTraceContext(t *testing.T) {
	t.Helper()
	prev := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTextMapPropagator(prev) })
}

// Behind otelhttp, TraceRequest continues the server span otelhttp opened — it
// must not replace it with the caller's span from the headers — and returns that
// span's traceparent.
func TestTraceRequestKeepsServerSpan(t *testing.T) {
	is := is.New(t)
	useTraceContext(t)

	tracer := sdktrace.NewTracerProvider().Tracer("test")
	// What otelhttp leaves in the request context.
	ctx, server := tracer.Start(context.Background(), "GET /x",
		trace.WithSpanKind(trace.SpanKindServer))
	defer server.End()
	sc := server.SpanContext()

	var parentOfChild trace.SpanContext
	var traceID string
	h := middleware.TraceRequest(tracer)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		traceID = skotel.GetTraceID(r.Context())
		_, child := skotel.AddSpan(r.Context(), "child")
		defer child.End()
		parentOfChild = child.(sdktrace.ReadOnlySpan).Parent()
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil).WithContext(ctx)
	req.Header.Set("traceparent", incoming)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	is.Equal(traceID, sc.TraceID().String())      // logs carry the server span's trace
	is.Equal(parentOfChild.SpanID(), sc.SpanID()) // child spans hang off the server span
	is.Equal(rec.Header().Get("traceparent"),     // the response points at it
		"00-"+sc.TraceID().String()+"-"+sc.SpanID().String()+"-01")
}

// Without a server span TraceRequest continues the caller's trace from the
// headers, and has no span of its own to return.
func TestTraceRequestContinuesIncomingTrace(t *testing.T) {
	is := is.New(t)
	useTraceContext(t)

	var traceID string
	h := middleware.TraceRequest(nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		traceID = skotel.GetTraceID(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("traceparent", incoming)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	is.Equal(traceID, "0af7651916cd43dd8448eb211c80319c")
	is.Equal(rec.Header().Get("traceparent"), "")
}

// With neither a span nor a traceparent header there is still a trace id to log.
func TestTraceRequestSeedsTraceID(t *testing.T) {
	is := is.New(t)
	useTraceContext(t)

	var traceID string
	h := middleware.TraceRequest(nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		traceID = skotel.GetTraceID(r.Context())
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	is.True(traceID != "")
	is.Equal(rec.Header().Get("traceparent"), "")
}
