package mid

import (
	"context"
	"net/http"
	"runtime/debug"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/logger"
	"github.com/assanoff/skit/rest"
)

// Panics returns application middleware that recovers a panic from a downstream
// typed handler (or inner middleware) and turns it into an *errs.Error of code
// Internal, so it flows through the normal error pipeline — Errors logs/traces
// it, MaskInternal hides it from clients in production, LocalizeErrors localizes
// it, and Respond encodes it as a proper JSON error instead of a bare empty 500.
//
// It logs the panic value and stack itself (a nil log skips that) and returns a
// detail-free Internal error, so the stack never reaches the client even when
// used alone. Place it innermost of the application middleware (closest to the
// handler) so the recovered error propagates back out through the rest of the
// chain. It complements, not replaces, a transport-level panic recovery (e.g.
// httplog) that guards raw handlers and other transport middleware.
func Panics(log *logger.Logger) rest.MidFunc {
	return func(next rest.HandlerFunc) rest.HandlerFunc {
		return func(ctx context.Context, r *http.Request) (resp rest.ResponseEncoder) {
			defer func() {
				if rec := recover(); rec != nil {
					if log != nil {
						log.Error(ctx, "panic recovered",
							"panic", rec, "stack", string(debug.Stack()),
							"method", r.Method, "path", r.URL.Path)
					}
					resp = errs.Newf(errs.Internal, "internal server error").
						WithMessageID("internal.panic")
				}
			}()
			return next(ctx, r)
		}
	}
}
