package broker_test

import (
	"context"
	"testing"

	"github.com/matryer/is"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/otel"
)

// errorType returns the error.type attribute of span.
func errorType(span sdktrace.ReadOnlySpan) string {
	for _, kv := range span.Attributes() {
		if kv.Key == "error.type" {
			return kv.Value.AsString()
		}
	}
	return ""
}

// The consumer span continues the trace the message was created in and links
// to it; the handler runs under it.
func TestInstrumentContinuesCreationTrace(t *testing.T) {
	is := is.New(t)
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	ctx, request := tp.Tracer("t").Start(t.Context(), "POST /widgets")
	created := request.SpanContext()
	msg := broker.Message{ID: "evt-1", Topic: "widgets", Headers: otel.Carrier(ctx)}
	request.End()
	var inSpan trace.SpanContext
	h := broker.Instrument(func(ctx context.Context, _ broker.Message) broker.Action {
		inSpan = trace.SpanContextFromContext(ctx)
		return broker.Ack
	}, broker.InstrumentConfig{System: "rabbitmq", TracerProvider: tp})

	h(t.Context(), msg)

	process := rec.Ended()[1]
	is.Equal(process.Name(), "process widgets")
	is.Equal(process.SpanKind(), trace.SpanKindConsumer)
	is.Equal(process.Parent().SpanID(), created.SpanID())
	is.Equal(process.SpanContext().TraceID(), created.TraceID())
	is.Equal(len(process.Links()), 1)
	is.Equal(process.Links()[0].SpanContext.SpanID(), created.SpanID())
	is.Equal(inSpan.SpanID(), process.SpanContext().SpanID())
	is.Equal(process.Status().Code, codes.Unset)
}

// A requeue, a discard and a panic mark the consumer span failed.
func TestInstrumentMarksFailedVerdicts(t *testing.T) {
	is := is.New(t)
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	h := broker.Instrument(verdicts(broker.Requeue, broker.Discard),
		broker.InstrumentConfig{TracerProvider: tp})
	body := ceBody(t, "order.paid")

	for range 3 {
		broker.Process(t.Context(), nil, "orders", body, nil, h)
	}

	spans := rec.Ended()
	is.Equal(len(spans), 3)
	is.Equal(spans[0].Name(), "process") // no topic routed
	is.True(!spans[0].Parent().IsValid())
	for i, want := range []string{"requeue", "discard", "panic"} {
		is.Equal(spans[i].Status().Code, codes.Error)
		is.Equal(errorType(spans[i]), want)
	}
}
