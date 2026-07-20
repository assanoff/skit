package middleware_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/matryer/is"

	"github.com/assanoff/skit/logger"
	"github.com/assanoff/skit/middleware"
)

func discardLogger() *logger.Logger {
	return logger.New(io.Discard, logger.Config{Service: "test", Level: logger.LevelError})
}

func TestCompressSetsVaryWhenGzipping(t *testing.T) {
	is := is.New(t)

	h := middleware.Compress()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "hello world hello world")
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	is.Equal(rec.Header().Get("Content-Encoding"), "gzip") // encoded
	is.Equal(rec.Header().Get("Vary"), "Accept-Encoding")  // and varied
}

func TestCompressNoVaryOnBodylessResponse(t *testing.T) {
	is := is.New(t)

	h := middleware.Compress()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent) // 204: never gzipped
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	is.Equal(rec.Code, http.StatusNoContent)
	is.Equal(rec.Header().Get("Content-Encoding"), "") // not encoded
	is.Equal(rec.Header().Get("Vary"), "")             // so no cache-fragmenting Vary
}

func TestPanicsRecoversWith500(t *testing.T) {
	is := is.New(t)

	h := middleware.Panics(discardLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil)) // must not propagate the panic

	is.Equal(rec.Code, http.StatusInternalServerError)
}

func TestPanicsNilLogger(t *testing.T) {
	is := is.New(t)

	h := middleware.Panics(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	is.Equal(rec.Code, http.StatusInternalServerError) // a nil logger still recovers
}

func TestPanicsPassesThroughWhenNoPanic(t *testing.T) {
	is := is.New(t)

	h := middleware.Panics(discardLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	is.Equal(rec.Code, http.StatusNoContent) // healthy request is untouched
}

func TestSizeLimitRejectsOversizedBody(t *testing.T) {
	is := is.New(t)

	var readErr error
	h := middleware.SizeLimit(4)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("0123456789")) // 10 bytes > 4
	h.ServeHTTP(rec, req)

	is.True(readErr != nil) // MaxBytesReader rejects the oversized body
}

func TestSizeLimitAllowsWithinLimit(t *testing.T) {
	is := is.New(t)

	var got string
	h := middleware.SizeLimit(100)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("small")))

	is.Equal(got, "small") // a body within the cap reads fully
}

func TestSizeLimitDisabledWhenNonPositive(t *testing.T) {
	is := is.New(t)

	var n int
	h := middleware.SizeLimit(0)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		n = len(b)
	}))

	rec := httptest.NewRecorder()
	big := strings.Repeat("x", 1000)
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(big)))

	is.Equal(n, 1000) // n<=0 disables the cap: the whole body is readable
}

func TestTimeoutSetsDeadline(t *testing.T) {
	is := is.New(t)

	var hadDeadline bool
	h := middleware.Timeout(50 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadDeadline = r.Context().Deadline()
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	is.True(hadDeadline) // the request context carries a deadline
}

func TestTimeoutCancelsContext(t *testing.T) {
	is := is.New(t)

	var ctxErr error
	h := middleware.Timeout(10 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		ctxErr = r.Context().Err()
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	is.Equal(ctxErr, context.DeadlineExceeded) // the context expires after d
}

func TestTimeoutDisabledWhenNonPositive(t *testing.T) {
	is := is.New(t)

	var hadDeadline bool
	h := middleware.Timeout(0)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadDeadline = r.Context().Deadline()
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	is.True(!hadDeadline) // d<=0 disables the middleware: no deadline added
}

func TestAccessLogWritesStructuredLine(t *testing.T) {
	is := is.New(t)

	var buf bytes.Buffer
	log := logger.New(&buf, logger.Config{Service: "test", Level: logger.LevelInfo})

	h := middleware.AccessLog(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("hello"))
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/things", nil))

	out := buf.String()
	is.True(strings.Contains(out, "http.request"))                    // the event was logged
	is.True(strings.Contains(out, `"http.response.status_code":201`)) // the wrapper captured the status
	is.True(strings.Contains(out, `"url.path":"/things"`))            // the path
	is.True(strings.Contains(out, `"http.response.body.size":5`))     // the wrapper counted the bytes
	is.Equal(rec.Code, http.StatusCreated)                            // downstream status is preserved
}

func TestAccessLogNilLogger(t *testing.T) {
	is := is.New(t)

	h := middleware.AccessLog(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	is.Equal(rec.Code, http.StatusTeapot) // a nil logger is a no-op wrapper
}

// TestPanicsPreservesFlusher guards against the wrapper hiding the underlying
// writer's optional interfaces: a handler behind Panics must still be able to
// Flush (SSE/streaming) via http.ResponseController, which walks the Unwrap
// chain to the real writer.
func TestPanicsPreservesFlusher(t *testing.T) {
	is := is.New(t)

	var called, flushed bool
	h := middleware.Panics(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		// httptest.ResponseRecorder implements http.Flusher; the Panics wrapper
		// must not mask it from ResponseController.
		flushed = http.NewResponseController(w).Flush() == nil
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	is.True(called)
	is.True(flushed) // Flush reached the underlying Flusher through Unwrap
}

// TestPanicsAfterWriteKeepsStatus checks that a handler which already started
// the response and then panics keeps its status: no superfluous 500 override.
func TestPanicsAfterWriteKeepsStatus(t *testing.T) {
	is := is.New(t)

	h := middleware.Panics(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial"))
		panic("boom after write")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	is.Equal(rec.Code, http.StatusOK) // the already-sent status is preserved, not overwritten with 500
}
