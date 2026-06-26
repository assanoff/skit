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
			defer func() {
				if rec := recover(); rec != nil {
					if log != nil {
						log.Error(r.Context(), "panic recovered",
							"panic", rec, "stack", string(debug.Stack()),
							"method", r.Method, "path", r.URL.Path)
					}
					w.WriteHeader(http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
