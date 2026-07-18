package middleware_test

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/assanoff/skit/middleware"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	})
}

func TestCORSAllowedOriginSetsHeaders(t *testing.T) {
	h := middleware.CORS(middleware.CORSConfig{AllowedOrigins: []string{"https://app.example.com"}})(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("allow-origin = %q, want the request origin", got)
	}
	if !strings.Contains(rec.Header().Get("Vary"), "Origin") {
		t.Errorf("expected Vary: Origin, got %q", rec.Header().Get("Vary"))
	}
}

func TestCORSDisallowedOriginNoHeaders(t *testing.T) {
	h := middleware.CORS(middleware.CORSConfig{AllowedOrigins: []string{"https://app.example.com"}})(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("disallowed origin must not receive Access-Control-Allow-Origin")
	}
	if rec.Body.String() != "hello" {
		t.Errorf("request should still pass through, body = %q", rec.Body.String())
	}
}

func TestCORSPreflightShortCircuits(t *testing.T) {
	h := middleware.CORS(middleware.CORSConfig{
		AllowedOrigins: []string{"*"},
		MaxAge:         600,
	})(okHandler())

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", rec.Code)
	}
	if rec.Body.String() != "" {
		t.Errorf("preflight must not reach the handler, body = %q", rec.Body.String())
	}
	if rec.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("preflight should set Access-Control-Allow-Methods")
	}
	if rec.Header().Get("Access-Control-Max-Age") != "600" {
		t.Errorf("max-age = %q, want 600", rec.Header().Get("Access-Control-Max-Age"))
	}
}

func TestCORSWildcardWithCredentialsEchoesOrigin(t *testing.T) {
	h := middleware.CORS(middleware.CORSConfig{
		AllowedOrigins:   []string{"*"},
		AllowCredentials: true,
	})(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("with credentials, allow-origin = %q, want the echoed origin (never *)", got)
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Error("expected Access-Control-Allow-Credentials: true")
	}
}

func TestSecureHeadersDefaults(t *testing.T) {
	h := middleware.SecureHeaders()(okHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	for k, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	} {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestSecureHeadersDoesNotOverride(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		_, _ = w.Write([]byte("x"))
	})
	h := middleware.SecureHeaders()(inner)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := rec.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Fatalf("X-Frame-Options = %q, want the handler's value to win", got)
	}
}

func TestRequestIDEchoesIncoming(t *testing.T) {
	h := middleware.RequestID()(okHandler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(middleware.RequestIDHeader, "abc-123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get(middleware.RequestIDHeader); got != "abc-123" {
		t.Fatalf("X-Request-ID = %q, want the incoming id echoed", got)
	}
}

func TestRequestIDNoneAvailable(t *testing.T) {
	h := middleware.RequestID()(okHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Header().Get(middleware.RequestIDHeader) != "" {
		t.Error("with no incoming id and no trace, X-Request-ID must not be synthesized")
	}
}

func TestCompressGzipsWhenAccepted(t *testing.T) {
	h := middleware.Compress()(okHandler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", rec.Header().Get("Content-Encoding"))
	}
	gr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	body, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("read gzip body: %v", err)
	}
	if string(body) != "hello" {
		t.Errorf("decompressed body = %q, want hello", body)
	}
}

func TestCompressSkippedWhenNotAccepted(t *testing.T) {
	h := middleware.Compress()(okHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Header().Get("Content-Encoding") == "gzip" {
		t.Fatal("must not gzip without Accept-Encoding: gzip")
	}
	if rec.Body.String() != "hello" {
		t.Errorf("body = %q, want plain hello", rec.Body.String())
	}
}
