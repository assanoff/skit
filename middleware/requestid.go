package middleware

import (
	"net/http"

	skotel "github.com/assanoff/skit/otel"
)

// RequestIDHeader is the header RequestID reads and echoes.
const RequestIDHeader = "X-Request-ID"

// RequestID echoes a request correlation id in the X-Request-ID response header.
// It reuses the client's incoming X-Request-ID when present; otherwise it falls
// back to the active trace id (so put this after TraceRequest in the chain). It
// never overwrites an id a handler set itself. When no id is available it does
// nothing — it does not synthesize one (tracing owns id generation).
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if w.Header().Get(RequestIDHeader) == "" {
				id := r.Header.Get(RequestIDHeader)
				if id == "" {
					id = skotel.GetTraceID(r.Context())
				}
				if id != "" {
					w.Header().Set(RequestIDHeader, id)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
