package otel

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

type ctxKey int

const (
	tracerKey ctxKey = iota + 1
	traceIDKey
)

const defaultTraceID = "00000000000000000000000000000000"

func setTracer(ctx context.Context, tracer trace.Tracer) context.Context {
	return context.WithValue(ctx, tracerKey, tracer)
}

func tracerFrom(ctx context.Context) trace.Tracer {
	if t, ok := ctx.Value(tracerKey).(trace.Tracer); ok {
		return t
	}
	return nil
}

func setTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey, traceID)
}

// GetTraceID returns the trace id stored in ctx, or an empty string if none.
// It is designed to be passed as logger.TraceIDFn so every log line carries the
// active trace id.
func GetTraceID(ctx context.Context) string {
	if id, ok := ctx.Value(traceIDKey).(string); ok {
		return id
	}
	return ""
}
