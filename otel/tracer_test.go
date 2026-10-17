package otel

import (
	"errors"
	"testing"

	"github.com/matryer/is"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

func TestTracerScope(t *testing.T) {
	is := is.New(t)
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))

	_, span := Tracer(tp, modulePath+"/dbx").Start(t.Context(), "query")
	span.End()

	scope := rec.Ended()[0].InstrumentationScope()
	is.Equal(scope.Name, modulePath+"/dbx")
	is.Equal(scope.SchemaURL, semconv.SchemaURL)
}

func TestTracerNilProviderIsGlobal(t *testing.T) {
	is := is.New(t)
	_, span := Tracer(nil, "scope").Start(t.Context(), "op")
	defer span.End()
	is.True(!span.SpanContext().IsValid()) // the global noop provider: no SDK installed
}

// codedError carries its own error.type, like *errs.Error.
type codedError struct{}

func (codedError) Error() string     { return "not found" }
func (codedError) ErrorType() string { return "NOT_FOUND" }

func TestSetError(t *testing.T) {
	is := is.New(t)
	rec := tracetest.NewSpanRecorder()
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)).Tracer("t")

	_, ok := tracer.Start(t.Context(), "ok")
	SetError(ok, nil)
	ok.End()
	_, failed := tracer.Start(t.Context(), "failed")
	SetError(failed, errors.Join(codedError{}))
	failed.End()

	spans := rec.Ended()
	is.Equal(spans[0].Status().Code, codes.Unset) // nil error changes nothing
	is.Equal(len(spans[0].Attributes()), 0)
	is.Equal(spans[1].Status().Code, codes.Error)
	is.Equal(spans[1].Status().Description, "not found")
	is.Equal(spans[1].Attributes()[0], semconv.ErrorTypeKey.String("NOT_FOUND"))
	is.Equal(len(spans[1].Events()), 0) // no exception event: RecordError is not used
}
