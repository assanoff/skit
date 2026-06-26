package mid

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/logger"
	"github.com/assanoff/skit/rest"
)

// Errors returns application middleware that observes server-fault responses: on
// an *errs.Error in the 5xx class it records the error on the active OpenTelemetry
// span (so the trace is marked failed and carries the error) and logs it
// server-side with the domain code, HTTP status, and detail.
//
// It is errs-aware and narrowed to a single responsibility — observability —
// rather than also masking or annotating. Masking is MaskInternal's job (place
// Errors inside it so it logs the original detail before masking), and the code
// location is omitted because our errs keeps it unexported. A nil log records
// only the span.
// 4xx (client-actionable) and non-error responses pass through untouched — the
// access log already covers those.
func Errors(log *logger.Logger) rest.MidFunc {
	return func(next rest.HandlerFunc) rest.HandlerFunc {
		return func(ctx context.Context, r *http.Request) rest.ResponseEncoder {
			resp := next(ctx, r)

			e, ok := resp.(*errs.Error)
			if !ok || e.HTTPStatus() < http.StatusInternalServerError {
				return resp
			}

			// Mark the active span failed and attach the error (a no-op span when
			// tracing is not active, so this is always safe).
			span := trace.SpanFromContext(ctx)
			span.RecordError(e)
			span.SetStatus(codes.Error, e.CodeStr)

			if log != nil {
				log.Error(ctx, "request failed",
					"code", e.CodeStr, "status", e.HTTPStatus(), "detail", e.Message)
			}
			return resp
		}
	}
}
