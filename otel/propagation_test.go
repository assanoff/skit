package otel

import (
	"testing"

	"github.com/matryer/is"
	"go.opentelemetry.io/otel/baggage"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Carrier holds the trace context only: baggage never reaches a message.
func TestCarrierRoundTripWithoutBaggage(t *testing.T) {
	is := is.New(t)
	tp := sdktrace.NewTracerProvider()
	ctx, span := tp.Tracer("t").Start(t.Context(), "publish")
	defer span.End()
	member, err := baggage.NewMember("user", "42")
	is.NoErr(err)
	bag, err := baggage.New(member)
	is.NoErr(err)
	ctx = baggage.ContextWithBaggage(ctx, bag)

	c := Carrier(ctx)
	is.Equal(len(c), 1) // traceparent only
	_, ok := c["baggage"]
	is.True(!ok)

	got := trace.SpanContextFromContext(ExtractFromCarrier(t.Context(), c))
	is.Equal(got.TraceID(), span.SpanContext().TraceID())
	is.Equal(got.SpanID(), span.SpanContext().SpanID())
	is.True(got.IsRemote())
}

func TestCarrierEmptyWithoutSpan(t *testing.T) {
	is := is.New(t)
	is.Equal(Carrier(t.Context()), nil)
}
