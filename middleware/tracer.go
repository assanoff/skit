package middleware

import (
	"net/http"

	"go.opentelemetry.io/otel/trace"

	skotel "github.com/assanoff/skit/otel"
)

// TraceRequest prepares the request context for tracing and logging. It
// continues the server span an earlier middleware opened (otelhttp), or — with
// no span in the context — the trace context of the incoming headers; stores
// tracer for otel.AddSpan; ensures a trace id is available for logging; and
// writes the server span's traceparent into the response (otel.InjectToResponse).
//
// TraceRequest opens no span and ends none: for a server span per request put
// otelhttp.NewMiddleware before it. Re-extracting the headers after otelhttp
// would replace its server span in the context with the caller's.
func TraceRequest(tracer trace.Tracer) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if !trace.SpanContextFromContext(ctx).IsValid() {
				ctx = skotel.ExtractFromRequest(ctx, r)
			}
			ctx = skotel.InjectTracing(ctx, tracer)
			skotel.InjectToResponse(ctx, w.Header())
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
