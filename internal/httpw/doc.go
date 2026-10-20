// Package httpw provides a single canonical http.ResponseWriter proxy shared by
// every middleware that needs to observe a response.
//
// Middlewares routinely wrap the ResponseWriter to capture the status code and
// byte count, or to tee the body for logging. Hand-rolling that wrapper in each
// middleware is both duplication and a trap: a naive wrapper silently hides the
// underlying writer's optional interfaces (http.Flusher, http.Hijacker, …), so
// an interposed middleware breaks SSE/streaming Flush and WebSocket Hijack.
//
// Writer sidesteps that by staying a single type and exposing the wrapped
// writer via Unwrap. Callers reach the optional interfaces through
// http.ResponseController, which walks the Unwrap chain — so no matter how
// middlewares are ordered, Flush/Hijack still find the real writer.
//
// # Usage
//
//	func Middleware(next http.Handler) http.Handler {
//		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//			ww := httpw.Wrap(w)
//			next.ServeHTTP(ww, r)
//			log.Printf("status=%d bytes=%d", ww.Status(), ww.BytesWritten())
//		})
//	}
//
// A handler behind this wrapper flushes with http.NewResponseController(w),
// not a w.(http.Flusher) type assertion.
//
// Status returns 0 until the handler writes a header or body, which lets a
// panic-recovery middleware tell "nothing sent yet" (safe to send 500) from
// "response already started" (must not).
package httpw
