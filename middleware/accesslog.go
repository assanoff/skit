package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/assanoff/skit/httpw"
)

// AccessLog logs one structured line per request with the request's context.
// With a logger built by logger.New or logger.NewWithHandler, each line also
// carries the trace fields of the request's span.
func AccessLog(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := httpw.Wrap(w)
			next.ServeHTTP(rec, r)
			if log != nil {
				// Field names follow OpenTelemetry HTTP semantic conventions.
				log.InfoContext(
					r.Context(), "http.request",
					"http.request.method", r.Method,
					"url.path", r.URL.Path,
					"http.response.status_code", rec.Status(),
					"http.response.body.size", rec.BytesWritten(),
					"duration_ms", time.Since(start).Milliseconds(),
				)
			}
		})
	}
}
