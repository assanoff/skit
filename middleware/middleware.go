package middleware

import (
	"net/http"
)

// Middleware is standard net/http middleware.
type Middleware = func(http.Handler) http.Handler

// statusRecorder captures the response status and byte count for access logs.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Unwrap exposes the underlying ResponseWriter so http.ResponseController can
// reach optional interfaces (Flusher/Hijacker/…) through this wrapper — e.g.
// SSE handlers that Flush per event. Without it, wrapping would mask Flush.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
