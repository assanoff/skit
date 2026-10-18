// Package runspan records a run of periodic work — a worker tick, a cron run —
// as the root span of its own trace.
package runspan

import (
	"context"

	"go.opentelemetry.io/otel/trace"

	"github.com/assanoff/skit/otel"
)

// Start opens the root span name. It lifts otel.Suppress: the sampler sees the
// values of ctx even for a new root, and a run that asked for a span must not
// lose it to a mark left on the context it was started from.
func Start(ctx context.Context, tracer trace.Tracer, name string) (context.Context, trace.Span) {
	return tracer.Start(otel.Unsuppress(ctx), name, //nolint:spancheck // the caller ends it with End
		trace.WithNewRoot(), trace.WithSpanKind(trace.SpanKindInternal))
}

// End ends span: a run that did not finish panicked, a run with err failed.
func End(span trace.Span, finished bool, err error) {
	switch {
	case !finished:
		otel.SetError(span, panicError{})
	case err != nil:
		otel.SetError(span, err)
	}
	span.End()
}

// panicError is the error of a run that panicked; its error.type is "panic".
type panicError struct{}

func (panicError) Error() string { return "panic" }

// ErrorType names the error for error.type.
func (panicError) ErrorType() string { return "panic" }
