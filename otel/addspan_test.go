package otel

import (
	"context"
	"testing"

	"github.com/matryer/is"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// Without a tracer in ctx AddSpan returns a no-op span, not the span already in
// ctx: ending it, as every caller does, must leave that span open.
func TestAddSpanNoTracerLeavesParentOpen(t *testing.T) {
	is := is.New(t)

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	// A span someone else started (a consumer, an HTTP server), with no tracer
	// injected into the context.
	ctx, parent := tp.Tracer("test").Start(context.Background(), "parent")

	gotCtx, span := AddSpan(ctx, "child")
	span.End()

	is.Equal(gotCtx, ctx)                  // ctx is returned unchanged
	is.True(!span.SpanContext().IsValid()) // a no-op span
	is.True(parent.IsRecording())          // the parent is still open
	is.Equal(len(sr.Ended()), 0)

	parent.End()
	is.Equal(len(sr.Ended()), 1) // ended once, by its owner
}
