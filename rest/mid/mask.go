package mid

import (
	"context"
	"net/http"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/logger"
	"github.com/assanoff/skit/rest"
)

// MaskInternal returns application middleware that keeps server-fault error
// details from leaking to clients. When the wrapped handler returns an
// *errs.Error in the 5xx class (HTTPStatus >= 500 — Internal, Unavailable, etc.)
// and mask is true (typically in production), it logs the original error
// server-side — so the detail survives for diagnosis — and replaces the response
// with a generic error that keeps the same code (hence status) but carries no
// detail. With mask false (e.g. in development) the original error passes through
// unchanged so you can read it. Non-5xx errors (4xx are client-actionable) and
// non-error responses always pass through; a nil log skips the server-side line.
//
// Place it inside LocalizeErrors (router.New(reqctx, LocalizeErrors,
// MaskInternal, ...)) so the localizer still runs on the masked error.
func MaskInternal(log *logger.Logger, mask bool) rest.MidFunc {
	return func(next rest.HandlerFunc) rest.HandlerFunc {
		return func(ctx context.Context, r *http.Request) rest.ResponseEncoder {
			resp := next(ctx, r)

			e, ok := resp.(*errs.Error)
			if !ok || e.HTTPStatus() < http.StatusInternalServerError {
				return resp
			}
			if !mask {
				return e
			}
			if log != nil {
				log.Error(ctx, "internal error masked", "code", e.CodeStr, "error", e.Error())
			}
			return errs.Newf(e.Code, "internal server error")
		}
	}
}
