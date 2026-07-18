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
// Accept-Encoding, and drops the (now-wrong) Content-Length. Compression starts
// lazily on the first Write, so empty/204 responses are untouched; a handler
// that already set Content-Encoding is left alone. Flusher is preserved.
func Compress() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") ||
				w.Header().Get("Content-Encoding") != "" {
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Add("Vary", "Accept-Encoding")

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

// gzipResponseWriter compresses body bytes on the way out. It defers the gzip
// setup to the first Write so a handler that writes nothing (204) never emits a
// gzip stream, and it delegates status/headers to the wrapped writer.
type gzipResponseWriter struct {
	http.ResponseWriter
	gz      *gzip.Writer
	started bool
}

func (w *gzipResponseWriter) WriteHeader(code int) {
	// Content-Length would describe the uncompressed size; gzip invalidates it.
	w.Header().Del("Content-Length")
	w.Header().Set("Content-Encoding", "gzip")
	w.ResponseWriter.WriteHeader(code)
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.started {
		w.started = true
		if w.Header().Get("Content-Encoding") == "" {
			w.WriteHeader(http.StatusOK)
		}
	}
	return w.gz.Write(b)
}

// Flush flushes the gzip stream then the underlying writer, so streaming
// responses (SSE) still reach the client promptly.
func (w *gzipResponseWriter) Flush() {
	if w.started {
		_ = w.gz.Flush()
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap exposes the underlying writer for http.ResponseController.
func (w *gzipResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
