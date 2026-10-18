package worker

import (
	"context"
	"testing"
	"time"

	"github.com/matryer/is"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/assanoff/skit/otel"
)

// recorder returns a provider that samples every span and the recorder of the
// spans it ends.
func recorder() (*sdktrace.TracerProvider, *tracetest.SpanRecorder) {
	rec := tracetest.NewSpanRecorder()
	return sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)), rec
}

// errorType returns the error.type attribute of span.
func errorType(span sdktrace.ReadOnlySpan) string {
	for _, kv := range span.Attributes() {
		if kv.Key == "error.type" {
			return kv.Value.AsString()
		}
	}
	return ""
}

// By default a tick runs suppressed and opens no span.
func TestLoopSuppressesTicks(t *testing.T) {
	is := is.New(t)
	tp, rec := recorder()
	var suppressed bool
	l := NewLoop(nil, LoopConfig{TracerProvider: tp}, func(ctx context.Context) error {
		suppressed = otel.IsSuppressed(ctx)
		return nil
	})

	l.runTick(t.Context())

	is.True(suppressed)
	is.Equal(len(rec.Ended()), 0)
}

// With Trace every tick is the root span of its own trace, named after the
// loop and not suppressed; an error and a panic mark it failed.
func TestLoopTracesTicks(t *testing.T) {
	is := is.New(t)
	tp, rec := recorder()
	var suppressed []bool
	step := sequence(succeed, fail, crash)
	l := NewLoop(nil, LoopConfig{Name: "report", Trace: true, TracerProvider: tp},
		func(ctx context.Context) error {
			suppressed = append(suppressed, otel.IsSuppressed(ctx))
			return step(ctx)
		})

	for range 3 {
		l.runTick(otel.Suppress(t.Context()))
	}

	is.Equal(suppressed, []bool{false, false, false})
	spans := rec.Ended()
	is.Equal(len(spans), 3)
	for _, s := range spans {
		is.Equal(s.Name(), "report")
		is.Equal(s.SpanKind(), trace.SpanKindInternal)
		is.True(!s.Parent().IsValid()) // a root
	}
	is.Equal(spans[0].Status().Code, codes.Unset)
	is.Equal(spans[1].Status().Code, codes.Error)
	is.Equal(errorType(spans[2]), "panic")
}

// markSink records whether the marks run suppressed.
type markSink struct {
	*memStore

	suppressed []bool
}

func (s *markSink) MarkDone(ctx context.Context, item memItem, now time.Time) error {
	s.suppressed = append(s.suppressed, otel.IsSuppressed(ctx))
	return s.memStore.MarkDone(ctx, item, now)
}

// A Processor lifts the mark of a suppressed tick for each item's handler and
// keeps it for the marks.
func TestProcessorLiftsSuppressionForItems(t *testing.T) {
	is := is.New(t)
	store := newMemStore(memItem{1}, memItem{2})
	sink := &markSink{memStore: store}
	var handled []bool
	p := NewProcessor[memItem](nil, store, HandlerFunc[memItem](func(ctx context.Context, _ memItem) error {
		handled = append(handled, otel.IsSuppressed(ctx))
		return nil
	}), sink, ProcessorConfig{Name: "test"})

	is.NoErr(p.Tick()(otel.Suppress(t.Context())))

	is.Equal(handled, []bool{false, false})
	is.Equal(sink.suppressed, []bool{true, true})
}
