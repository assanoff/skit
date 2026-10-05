package otel

import (
	"context"
	"net/http"
	"testing"

	"github.com/matryer/is"
	"go.opentelemetry.io/otel/baggage"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// The response carries the traceparent of this process's own span, and never
// the baggage that travels with it.
func TestInjectToResponseWritesOwnSpan(t *testing.T) {
	is := is.New(t)

	tp := sdktrace.NewTracerProvider()
	ctx, span := tp.Tracer("test").Start(context.Background(), "GET /x")
	defer span.End()
	member, err := baggage.NewMember("user", "42")
	is.NoErr(err)
	bag, err := baggage.New(member)
	is.NoErr(err)
	ctx = baggage.ContextWithBaggage(ctx, bag)

	h := http.Header{}
	InjectToResponse(ctx, h)

	sc := span.SpanContext()
	is.Equal(h.Get("traceparent"), "00-"+sc.TraceID().String()+"-"+sc.SpanID().String()+"-01")
	is.Equal(h.Get("baggage"), "") // request-scoped data stays on the server
}

// A remote parent is the caller's span: echoing it back would point at the
// wrong span, so nothing is written — nor without any span.
func TestInjectToResponseSkipsForeignSpan(t *testing.T) {
	is := is.New(t)

	remote := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1},
		SpanID:     trace.SpanID{2},
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	for _, ctx := range []context.Context{
		trace.ContextWithRemoteSpanContext(context.Background(), remote),
		context.Background(),
	} {
		h := http.Header{}
		InjectToResponse(ctx, h)
		is.Equal(len(h), 0)
	}
}
