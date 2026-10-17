package mid

import (
	"context"
	"log/slog"
	"net/http"

	"go.opentelemetry.io/otel/trace"

	"github.com/assanoff/skit/errs"
	skotel "github.com/assanoff/skit/otel"
	"github.com/assanoff/skit/rest"
)

// Errors returns application middleware that observes server-fault responses: on
// an *errs.Error in the 5xx class it marks the active OpenTelemetry span failed
// with otel.SetError, so the span carries the domain code as error.type, and logs
// the error server-side with the domain code, HTTP status, and detail.
//
// It is errs-aware and narrowed to a single responsibility — observability —
// rather than also masking or annotating. Masking is MaskInternal's job (place
// Errors inside it so it logs the original detail before masking), and the code
// location is omitted because our errs keeps it unexported. A nil log records
// only the span.
// 4xx (client-actionable) and non-error responses pass through untouched — the
// access log already covers those.
func Errors(log *slog.Logger) rest.MidFunc {
	return func(next rest.HandlerFunc) rest.HandlerFunc {
		return func(ctx context.Context, r *http.Request) rest.ResponseEncoder {
			resp := next(ctx, r)

			e, ok := resp.(*errs.Error)
			if !ok || e.HTTPStatus() < http.StatusInternalServerError {
				return resp
			}

			// The status description is replaced by otelhttp from the response
			// code; error.type and the log line below carry the detail.
			skotel.SetError(trace.SpanFromContext(ctx), e)

			if log != nil {
				log.ErrorContext(ctx, "request failed",
					"code", e.CodeStr, "status", e.HTTPStatus(), "detail", e.Message)
			}
			return resp
		}
	}
}
