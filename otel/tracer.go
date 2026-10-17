package otel

import (
	"cmp"
	"runtime/debug"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

// modulePath is the import path of the skit module, the parent of every scope.
const modulePath = "github.com/assanoff/skit"

// moduleVersion is the skit version the binary was built with; empty when the
// build carries no module information.
var moduleVersion = sync.OnceValue(func() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	if bi.Main.Path == modulePath {
		return bi.Main.Version
	}
	for _, dep := range bi.Deps {
		if dep.Path == modulePath {
			return dep.Version
		}
	}
	return ""
})

// Tracer returns the tracer an SDK package records its spans with. scope is
// the package import path; the tracer carries the skit module version and the
// semconv schema URL. A nil tp means the global provider: a tracer taken from it
// before the SDK is installed switches to the SDK once it is, so a component
// takes its tracer once, in its constructor.
func Tracer(tp trace.TracerProvider, scope string) trace.Tracer {
	tp = cmp.Or(tp, otel.GetTracerProvider())
	return tp.Tracer(scope,
		trace.WithInstrumentationVersion(moduleVersion()),
		trace.WithSchemaURL(semconv.SchemaURL),
	)
}

// SetError marks span as failed: status Error with the error text and
// error.type. error.type comes from an ErrorType() string method anywhere in
// the err chain, else from the error's type. A nil err does nothing.
//
// The exception details belong to the log record written with the "err" key,
// not to the span: SetError replaces span.RecordError.
func SetError(span trace.Span, err error) {
	if err == nil {
		return
	}
	span.SetStatus(codes.Error, err.Error())
	span.SetAttributes(semconv.ErrorType(err))
}
