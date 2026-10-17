package middleware

import (
	"net/http"

	"go.opentelemetry.io/otel/trace"
)

// RequestIDHeader is the header RequestID reads and echoes.
const RequestIDHeader = "X-Request-ID"

// RequestID echoes a request correlation id in the X-Request-ID response header.
// It reuses the client's incoming X-Request-ID when present; otherwise it falls
// back to the trace id of the request's span (so put it after Trace in the
// chain). It never overwrites an id a handler set itself. When no id is
// available it does nothing — it does not synthesize one (tracing owns id
// generation).
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if id := requestID(r); id != "" && w.Header().Get(RequestIDHeader) == "" {
				w.Header().Set(RequestIDHeader, id)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requestID returns the incoming X-Request-ID, else the trace id of the span in
// the request context, else "".
func requestID(r *http.Request) string {
	if id := r.Header.Get(RequestIDHeader); id != "" {
		return id
	}
	if sc := trace.SpanContextFromContext(r.Context()); sc.HasTraceID() {
		return sc.TraceID().String()
	}
	return ""
}
