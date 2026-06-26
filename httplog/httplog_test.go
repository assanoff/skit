package httplog

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/logger"
)

func TestCURL(t *testing.T) {
	is := is.New(t)

	// GET: no -X, no body, headers rendered as -H.
	get := httptest.NewRequest(http.MethodGet, "/widgets?q=1", nil)
	get.Header.Set("Accept", "application/json")
	cmd := CURL(get, "")
	is.True(strings.HasPrefix(cmd, "curl ")) // no -X for GET
	is.True(strings.Contains(cmd, "'http://example.com/widgets?q=1'"))
	is.True(strings.Contains(cmd, "-H 'Accept: application/json'"))
	is.True(!strings.Contains(cmd, "--data-raw")) // GET carries no body

	// POST: body rendered as --data-raw, still no -X.
	post := httptest.NewRequest(http.MethodPost, "/widgets", strings.NewReader("{}"))
	postCmd := CURL(post, `{"name":"x"}`)
	is.True(!strings.Contains(postCmd, "-X"))
	is.True(strings.Contains(postCmd, `--data-raw '{"name":"x"}'`))

	// Other methods get an explicit -X.
	put := httptest.NewRequest(http.MethodPut, "/widgets/1", nil)
	is.True(strings.Contains(CURL(put, ""), "-X PUT"))

	// Single quotes in a value are shell-escaped.
	is.True(strings.Contains(CURL(post, "a'b"), `--data-raw 'a'\''b'`))
}

func TestWrapResponseWriterStatusAndBytes(t *testing.T) {
	is := is.New(t)

	rec := httptest.NewRecorder()
	ww := NewWrapResponseWriter(rec, 1)

	is.Equal(ww.Status(), 0) // nothing written yet

	ww.WriteHeader(http.StatusCreated)
	n, err := ww.Write([]byte("hello"))
	is.NoErr(err)
	is.Equal(n, 5)

	is.Equal(ww.Status(), http.StatusCreated)
	is.Equal(ww.BytesWritten(), 5)
	is.Equal(rec.Code, http.StatusCreated) // proxied to the underlying writer
	is.Equal(rec.Body.String(), "hello")
}

func TestWrapResponseWriterImplicitOK(t *testing.T) {
	is := is.New(t)

	rec := httptest.NewRecorder()
	ww := NewWrapResponseWriter(rec, 1)
	_, _ = ww.Write([]byte("x"))

	is.Equal(ww.Status(), http.StatusOK) // a Write with no WriteHeader implies 200
}

func TestWrapResponseWriterTee(t *testing.T) {
	is := is.New(t)

	rec := httptest.NewRecorder()
	ww := NewWrapResponseWriter(rec, 1)

	var tee bytes.Buffer
	ww.Tee(&tee)
	_, _ = ww.Write([]byte("dup"))

	is.Equal(rec.Body.String(), "dup") // proxied to the original
	is.Equal(tee.String(), "dup")      // and copied to the tee
}

func TestClientIP(t *testing.T) {
	is := is.New(t)

	mk := func(remote string, headers map[string]string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = remote
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		return r
	}

	is.Equal(clientIP(mk("1.2.3.4:5678", nil)), "1.2.3.4:5678")                                      // falls back to RemoteAddr
	is.Equal(clientIP(mk("x", map[string]string{"X-Real-IP": "9.9.9.9"})), "9.9.9.9")                // then X-Real-IP
	is.Equal(clientIP(mk("x", map[string]string{"X-Forwarded-For": "8.8.8.8, 7.7.7.7"})), "8.8.8.8") // first XFF hop, trimmed
}

func TestLogBody(t *testing.T) {
	is := is.New(t)

	jsonHdr := http.Header{}
	jsonHdr.Set("Content-Type", "application/json")

	// A whitelisted content type is logged verbatim.
	is.Equal(logBody(bytes.NewBufferString(`{"a":1}`), jsonHdr, &defaultOptions), `{"a":1}`)

	// An empty body is the empty string.
	is.Equal(logBody(bytes.NewBufferString(""), jsonHdr, &defaultOptions), "")

	// A non-whitelisted content type is redacted. (Note: defaultOptions whitelists
	// "", and HasPrefix(_, "") matches everything — so a redaction test must use a
	// whitelist that excludes the empty entry.)
	jsonOnly := &Options{LogBodyContentTypes: []string{"application/json"}, LogBodyMaxLen: 1024}
	binHdr := http.Header{}
	binHdr.Set("Content-Type", "application/octet-stream")
	is.True(strings.Contains(logBody(bytes.NewBufferString("data"), binHdr, jsonOnly), "redacted"))

	// A body beyond LogBodyMaxLen is trimmed.
	short := &Options{LogBodyContentTypes: []string{"application/json"}, LogBodyMaxLen: 3}
	trimmed := logBody(bytes.NewBufferString("123456"), jsonHdr, short)
	is.True(strings.HasPrefix(trimmed, "123"))
	is.True(strings.HasSuffix(trimmed, "... [trimmed]"))
}

func TestAppendAttrsSkipsEmptyKey(t *testing.T) {
	is := is.New(t)

	out := appendAttrs(nil, slog.String("a", "1"), slog.Attr{}, slog.String("b", "2"))
	is.Equal(len(out), 2) // the zero-key attr is dropped
}

func TestGroupAttrs(t *testing.T) {
	is := is.New(t)

	in := []slog.Attr{
		slog.String("http.method", "GET"),
		slog.String("http.path", "/x"),
		slog.String("flat", "v"),
	}
	out := groupAttrs(in, ".")

	keys := map[string]bool{}
	for _, a := range out {
		keys[a.Key] = true
	}
	is.Equal(len(out), 2) // the two http.* collapse into one group, flat stays
	is.True(keys["flat"])
	is.True(keys["http"])
}

func TestRequestLoggerLogsRequestWithCustomAttr(t *testing.T) {
	is := is.New(t)

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	h := RequestLogger(log, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetAttrs(r.Context(), slog.String("user_id", "u1"))
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/widgets", nil))

	out := buf.String()
	is.Equal(rec.Code, http.StatusCreated)
	is.True(strings.Contains(out, "GET /widgets => HTTP 201"))        // the summary message
	is.True(strings.Contains(out, `"http.response.status_code":201`)) // mapped via the ECS schema
	is.True(strings.Contains(out, `"user_id":"u1"`))                  // the SetAttrs attribute is included
}

func TestRequestLoggerSetError(t *testing.T) {
	is := is.New(t)

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	h := RequestLogger(log, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = SetError(r.Context(), errors.New("kaboom"))
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	is.True(strings.Contains(buf.String(), "kaboom")) // SetError surfaced the error in the log
}

func TestRequestLoggerRecoversPanic(t *testing.T) {
	is := is.New(t)

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))

	h := RequestLogger(log, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil)) // must not propagate the panic

	is.Equal(rec.Code, http.StatusInternalServerError) // RecoverPanics defaults to true -> 500
	is.True(strings.Contains(buf.String(), "panic: boom"))
}

func TestMiddlewareUsesServicekitLogger(t *testing.T) {
	is := is.New(t)

	var buf bytes.Buffer
	log := logger.New(&buf, logger.Config{Service: "test", Level: logger.LevelInfo})

	h := Middleware(log, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ping", nil))

	is.Equal(rec.Code, http.StatusOK)
	is.True(strings.Contains(buf.String(), "GET /ping => HTTP 200")) // logs through the skit logger's handler
}
