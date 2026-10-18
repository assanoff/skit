package brokertrace_test

import (
	"testing"

	"github.com/matryer/is"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/assanoff/skit/broker/internal/brokertrace"
	"github.com/assanoff/skit/otel"
)

func recorder() (trace.Tracer, *tracetest.SpanRecorder) {
	rec := tracetest.NewSpanRecorder()
	return sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)).Tracer("t"), rec
}

// A message without a trace context gets the PRODUCER span as its creation
// context; the caller's headers stay untouched.
func TestStartSendCreatesContext(t *testing.T) {
	is := is.New(t)
	tracer, rec := recorder()
	in := map[string]string{"x-tenant": "acme"}

	_, span, out := brokertrace.StartSend(t.Context(), tracer, "orders", in)
	brokertrace.End(span, nil)

	sent := rec.Ended()[0]
	is.Equal(sent.Name(), "send orders")
	is.Equal(sent.SpanKind(), trace.SpanKindProducer)
	is.Equal(brokertrace.Created(out).SpanID(), sent.SpanContext().SpanID()) // the span goes out
	is.Equal(out["x-tenant"], "acme")
	is.Equal(len(in), 1) // the caller's map is not written
}

// A message that carries its creation context — as the outbox relay sends an
// event — is sent under a CLIENT child of it, with its headers as they are.
func TestStartSendKeepsCreationContext(t *testing.T) {
	is := is.New(t)
	tracer, rec := recorder()
	ctx, request := tracer.Start(t.Context(), "POST /widgets")
	in := otel.Carrier(ctx)
	request.End()

	_, span, out := brokertrace.StartSend(t.Context(), tracer, "orders", in)
	brokertrace.End(span, nil)

	sent := rec.Ended()[1]
	is.Equal(sent.SpanKind(), trace.SpanKindClient)
	is.Equal(sent.Parent().SpanID(), request.SpanContext().SpanID())
	is.Equal(sent.SpanContext().TraceID(), request.SpanContext().TraceID())
	is.Equal(out, in) // the creation context goes out unchanged
}
