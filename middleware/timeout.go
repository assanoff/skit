package middleware

import (
	"context"
	"net/http"
	"time"
)

// Timeout cancels the request context after d and (if nothing was written) lets
// downstream handlers observe ctx.Err. A non-positive d disables the middleware.
func Timeout(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		if d <= 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
