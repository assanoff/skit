package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/assanoff/skit/logger"
)

// Panics recovers panics, logs the stack, and responds with 500.
func Panics(log *logger.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{ResponseWriter: w}
			defer func() {
				if p := recover(); p != nil {
					if log != nil {
						log.Error(r.Context(), "panic recovered",
							"panic", p, "stack", string(debug.Stack()),
							"method", r.Method, "path", r.URL.Path)
					}
					// Only send 500 if the handler hadn't already started writing
					// a response; otherwise this is a superfluous WriteHeader that
					// is dropped with a warning (and the real status is lost).
					if rec.status == 0 {
						rec.WriteHeader(http.StatusInternalServerError)
					}
				}
			}()
			next.ServeHTTP(rec, r)
		})
	}
}
