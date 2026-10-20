package middleware

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/assanoff/skit/internal/httpw"
	"github.com/assanoff/skit/logger"
)

// Panics recovers a panic, logs it once with the stack, and responds with 500.
// Register it once, outermost (router root Use), so it catches whatever the
// layers below it do not; an access log further in that recovers panics
// itself (httplog RecoverPanics) records the 500 first.
//
// http.ErrAbortHandler is panicked again: it asks net/http to abort the
// response, not to answer it.
func Panics(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Reuse a recording writer from up the chain (metrics, httplog):
			// every write still goes through it, without a wrapper per request.
			rec, ok := w.(*httpw.Writer)
			if !ok {
				rec = httpw.Wrap(w)
			}
			ctx := r.Context()
			defer func() {
				p := recover()
				if p == nil {
					return
				}
				if err, ok := p.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(p)
				}
				if log != nil {
					log.ErrorContext(ctx, "panic recovered",
						logger.PanicArgs(p, "method", r.Method, "path", r.URL.Path)...)
				}
				// Only send 500 if the handler hadn't already started writing
				// a response; otherwise this is a superfluous WriteHeader that
				// is dropped with a warning (and the real status is lost).
				if rec.Status() == 0 {
					rec.WriteHeader(http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(rec, r)
		})
	}
}
