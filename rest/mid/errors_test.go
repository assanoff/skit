package mid_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matryer/is"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/rest"
	"github.com/assanoff/skit/rest/mid"
)

// TestErrorsLogs5xx verifies a 5xx error is logged with its code and detail.
func TestErrorsLogs5xx(t *testing.T) {
	is := is.New(t)

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	got := run(mid.Errors(log), errs.Newf(errs.Internal, "db exploded"))
	_, ok := got.(*errs.Error)
	is.True(ok) // response passes through unchanged

	logged := buf.String()
	is.True(strings.Contains(logged, "request failed")) // logged
	is.True(strings.Contains(logged, "db exploded"))    // with the detail
	is.True(strings.Contains(logged, "internal"))       // and the code
}

// TestErrorsSkips4xx verifies a 4xx error is not logged by Errors (the access
// log covers those).
func TestErrorsSkips4xx(t *testing.T) {
	is := is.New(t)

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	got := run(mid.Errors(log), errs.Newf(errs.NotFound, "missing"))
	_, ok := got.(*errs.Error)
	is.True(ok)
	is.Equal(buf.Len(), 0) // 4xx not logged here
}

// TestErrorsPassThroughSuccess verifies a success response is untouched and
// unlogged.
func TestErrorsPassThroughSuccess(t *testing.T) {
	is := is.New(t)

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	got := run(mid.Errors(log), rest.JSON("ok"))
	_, isErr := got.(*errs.Error)
	is.True(!isErr)
	is.Equal(buf.Len(), 0)
}

// TestErrorsChainMasksAfterLog verifies Errors logs the original detail while
// MaskInternal hides it from the client (Errors inside MaskInternal).
func TestErrorsChainMasksAfterLog(t *testing.T) {
	is := is.New(t)

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	// outermost..innermost: MaskInternal, Errors
	h := rest.ChainMiddleware(func(_ context.Context, _ *http.Request) rest.ResponseEncoder {
		return errs.Newf(errs.Internal, "secret detail")
	}, mid.MaskInternal(nil, true), mid.Errors(log))

	resp := h(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil))
	e, ok := resp.(*errs.Error)
	is.True(ok)
	is.Equal(e.Message, "internal server error")             // client sees masked
	is.True(strings.Contains(buf.String(), "secret detail")) // log keeps original
}

// A 5xx error marks the request's span failed with the domain code as
// error.type and adds no exception event; a 4xx leaves the span alone.
func TestErrorsMarksSpan(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     *errs.Error
		status  codes.Code
		errType string
	}{
		{
			name:    "server fault",
			err:     errs.Newf(errs.Internal, "db exploded"),
			status:  codes.Error,
			errType: "internal",
		},
		{
			name:   "client error",
			err:    errs.Newf(errs.NotFound, "missing"),
			status: codes.Unset,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			is := is.New(t)
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			ctx, span := tp.Tracer("test").Start(t.Context(), "GET /x")
			h := mid.Errors(nil)(
				func(context.Context, *http.Request) rest.ResponseEncoder {
					return tc.err
				})

			h(ctx, httptest.NewRequestWithContext(ctx, http.MethodGet, "/x", nil))
			span.End()

			got := rec.Ended()
			is.Equal(len(got), 1)
			is.Equal(got[0].Status().Code, tc.status)
			is.Equal(len(got[0].Events()), 0) // no exception event
			errType := ""
			for _, a := range got[0].Attributes() {
				if a.Key == semconv.ErrorTypeKey {
					errType = a.Value.AsString()
				}
			}
			is.Equal(errType, tc.errType)
		})
	}
}
