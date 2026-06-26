package middleware

import (
	"net/http"

	"go.opentelemetry.io/otel/trace"

	skotel "github.com/assanoff/skit/otel"
)

// TraceRequest seeds the request context with trace context extracted from
// incoming headers and ensures a trace id is available for logging.
func TraceRequest(tracer trace.Tracer) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := skotel.ExtractFromRequest(r.Context(), r)
			ctx = skotel.InjectTracing(ctx, tracer)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
