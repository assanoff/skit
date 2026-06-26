package middleware

import (
	"net/http"
	"time"

	"github.com/assanoff/skit/logger"
)

// AccessLog logs one structured line per request. Because it logs through the
// skit logger, each line carries the request's trace_id automatically.
func AccessLog(log *logger.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			if log != nil {
				// Field names follow OpenTelemetry HTTP semantic conventions.
				log.Info(
					r.Context(), "http.request",
					"http.request.method", r.Method,
					"url.path", r.URL.Path,
					"http.response.status_code", rec.status,
					"http.response.body.size", rec.bytes,
					"duration_ms", time.Since(start).Milliseconds(),
				)
			}
		})
	}
}
