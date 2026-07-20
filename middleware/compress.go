package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

var gzipPool = sync.Pool{
	New: func() any { return gzip.NewWriter(io.Discard) },
}

// Compress returns middleware that gzip-encodes responses when the client sends
// "Accept-Encoding: gzip". It sets Content-Encoding: gzip and Vary:
// Accept-Encoding, and drops the (now-wrong) Content-Length. Bodyless responses
// (1xx, 204, 304) are never encoded, and a handler that already set
// Content-Encoding is left alone. Flusher is preserved.
func Compress() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") ||
				w.Header().Get("Content-Encoding") != "" {
				next.ServeHTTP(w, r)
				return
			}

			gz := gzipPool.Get().(*gzip.Writer)
			gz.Reset(w)
			cw := &gzipResponseWriter{ResponseWriter: w, gz: gz}
			defer func() {
				if cw.started {
					_ = gz.Close()
				}
				gzipPool.Put(gz)
			}()

			next.ServeHTTP(cw, r)
		})
	}
}

// gzipResponseWriter compresses body bytes on the way out. It decides whether to
// encode at WriteHeader time, based on the status code: bodyless responses (1xx,
// 204, 304) and any response whose handler already set Content-Encoding are
// passed through untouched (no gzip stream, Content-Length preserved). For
// gzippable responses it sets Content-Encoding: gzip and drops the now-wrong
// Content-Length.
type gzipResponseWriter struct {
	http.ResponseWriter
	gz          *gzip.Writer
	wroteHeader bool
	started     bool // gzip is active; false means passthrough
}

func (w *gzipResponseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	w.wroteHeader = true
	if bodyAllowed(code) && w.Header().Get("Content-Encoding") == "" {
		// Content-Length would describe the uncompressed size; gzip invalidates it.
		w.Header().Del("Content-Length")
		w.Header().Set("Content-Encoding", "gzip")
		// Vary only when we actually encode, so bodyless (204/304/1xx) or
		// pre-encoded passthroughs do not fragment shared caches.
		w.Header().Add("Vary", "Accept-Encoding")
		w.started = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if !w.started {
		return w.ResponseWriter.Write(b) // passthrough: 204/304/1xx or pre-set encoding
	}
	return w.gz.Write(b)
}

// bodyAllowed reports whether a response with this status code may carry a body
// we can gzip. 1xx, 204 (No Content) and 304 (Not Modified) must not be encoded.
func bodyAllowed(code int) bool {
	switch {
	case code >= 100 && code < 200:
		return false
	case code == http.StatusNoContent, code == http.StatusNotModified:
		return false
	}
	return true
}

// Flush flushes the gzip stream then the underlying writer, so streaming
// responses (SSE) still reach the client promptly. It reaches the underlying
// Flusher via http.ResponseController, which walks the Unwrap chain, so it
// works regardless of how this middleware is ordered against other wrappers.
func (w *gzipResponseWriter) Flush() {
	if w.started {
		_ = w.gz.Flush()
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

// Unwrap exposes the underlying writer for http.ResponseController.
func (w *gzipResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
