package httpw

import (
	"io"
	"net/http"
)

// Writer is an http.ResponseWriter proxy that records the response status code
// and byte count and can tee the body to a second writer.
//
// It re-exposes the wrapped writer via Unwrap, so downstream code reaches the
// underlying writer's optional interfaces (http.Flusher, http.Hijacker, …)
// through http.ResponseController — which walks the Unwrap chain — instead of
// through this wrapper. That keeps Writer a single type rather than one per
// interface combination.
type Writer struct {
	http.ResponseWriter
	tee         io.Writer
	code        int
	bytes       int
	wroteHeader bool
}

// Wrap wraps w so a middleware can observe the response. Callers that need to
// Flush or Hijack should use http.NewResponseController(w), not a type
// assertion — the Writer intentionally does not re-implement those interfaces.
func Wrap(w http.ResponseWriter) *Writer {
	return &Writer{ResponseWriter: w}
}

// WriteHeader records the first non-1xx status code and forwards every call.
// Informational (1xx) responses are forwarded but not recorded as the final
// status, matching net/http's interim-response handling; 101 Switching
// Protocols is treated as a terminal status.
func (w *Writer) WriteHeader(code int) {
	if code >= 100 && code <= 199 && code != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	if !w.wroteHeader {
		w.code = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

// Write proxies the body: it implies a 200 on the first write when the handler
// never called WriteHeader, counts the bytes, and mirrors them to the tee when
// one is set.
func (w *Writer) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(b)
	if w.tee != nil {
		_, _ = w.tee.Write(b[:n])
	}
	w.bytes += n
	return n, err
}

// Status returns the recorded status code, or 0 if the handler has not written
// a header or body yet.
func (w *Writer) Status() int { return w.code }

// BytesWritten returns the number of body bytes written to the client.
func (w *Writer) BytesWritten() int { return w.bytes }

// Tee mirrors every subsequent body write to dst in addition to the client. It
// is illegal to modify dst concurrently with writes.
func (w *Writer) Tee(dst io.Writer) { w.tee = dst }

// Unwrap returns the wrapped writer so http.ResponseController can reach its
// optional interfaces.
func (w *Writer) Unwrap() http.ResponseWriter { return w.ResponseWriter }
