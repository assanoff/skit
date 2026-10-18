package runspan_test

import (
	"log/slog"
	"testing"

	"github.com/matryer/is"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/assanoff/skit/internal/runspan"
	"github.com/assanoff/skit/otel"
	"github.com/assanoff/skit/otel/setup"
)

// A run started from a suppressed context is still sampled, while a span
// started there directly is dropped.
func TestStartLiftsSuppression(t *testing.T) {
	is := is.New(t)
	tel, err := setup.New(t.Context(), slog.New(slog.DiscardHandler), setup.Config{
		Sampler:  sdktrace.AlwaysSample(),
		Exporter: tracetest.NewInMemoryExporter(),
	})
	is.NoErr(err)
	t.Cleanup(func() { _ = tel.Shutdown() })
	tracer := tel.TracerProvider().Tracer("t")
	ctx := otel.Suppress(t.Context())

	_, dropped := tracer.Start(ctx, "query")
	defer dropped.End()
	_, run := runspan.Start(ctx, tracer, "report")
	defer run.End()

	// The mark drops a plain span; the run lifts it and starts a root.
	is.True(!dropped.SpanContext().IsSampled())
	is.True(run.SpanContext().IsSampled())
	is.True(!run.(sdktrace.ReadOnlySpan).Parent().IsValid())
}
