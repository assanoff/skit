package otel

import (
	"context"
	"testing"
	"time"

	"github.com/matryer/is"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestAddSpanAtBackdates(t *testing.T) {
	is := is.New(t)

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	ctx := InjectTracing(context.Background(), tp.Tracer("test"))

	start := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Second)
	_, span := AddSpanAt(ctx, "historical.op", start, end, attribute.String("k", "v"))
	is.True(span.SpanContext().IsValid())

	ended := sr.Ended()
	is.Equal(len(ended), 1) // the span is started and ended within the call
	s := ended[0]
	is.Equal(s.Name(), "historical.op")
	is.True(s.StartTime().Equal(start)) // backdated start
	is.True(s.EndTime().Equal(end))     // backdated end
}

// Nested AddSpanAt parents to the returned context even though the parent span is
// already ended (reconstructing a historical tree).
func TestAddSpanAtNests(t *testing.T) {
	is := is.New(t)

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	ctx := InjectTracing(context.Background(), tp.Tracer("test"))

	base := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	pctx, parent := AddSpanAt(ctx, "parent", base, base.Add(time.Second))
	_, child := AddSpanAt(pctx, "child", base, base.Add(500*time.Millisecond))

	is.Equal(child.SpanContext().TraceID(), parent.SpanContext().TraceID()) // same trace
	is.Equal(len(sr.Ended()), 2)
}

// With no tracer in the context AddSpanAt is a no-op returning an invalid span.
func TestAddSpanAtNoTracerNoOp(t *testing.T) {
	is := is.New(t)
	base := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	_, span := AddSpanAt(context.Background(), "x", base, base.Add(time.Second))
	is.True(!span.SpanContext().IsValid())
}
