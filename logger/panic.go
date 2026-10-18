package logger

import (
	"fmt"
	"runtime/debug"

	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

// PanicArgs returns the arguments a recovered panic p is logged with, followed
// by args: err, the error value "panic: <p>", and exception.stacktrace, the
// stack of the goroutine that recovered it. Call it in the deferred function:
//
//	if p := recover(); p != nil {
//		log.ErrorContext(ctx, "panic recovered", logger.PanicArgs(p, "path", r.URL.Path)...)
//	}
func PanicArgs(p any, args ...any) []any {
	return append([]any{
		"err", fmt.Errorf("panic: %v", p),
		string(semconv.ExceptionStacktraceKey), string(debug.Stack()),
	}, args...)
}
