package debugsrv

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlerServesPprof(t *testing.T) {
	h := Handler(Config{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("pprof index status = %d, want 200", rec.Code)
	}
}

func TestHandlerMountsOptionalEndpoints(t *testing.T) {
	ok := func(body string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		})
	}
	h := Handler(Config{
		MetricsHandler: ok("metrics"),
		Liveness:       ok("live"),
		Readiness:      ok("ready"),
	})

	for path, want := range map[string]string{
		"/metrics": "metrics",
		"/healthz": "live",
		"/readyz":  "ready",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != want {
			t.Fatalf("%s = (%d,%q), want (200,%q)", path, rec.Code, rec.Body.String(), want)
		}
	}
}

func TestHandlerOmitsUnsetEndpoints(t *testing.T) {
	h := Handler(Config{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/metrics with no handler = %d, want 404", rec.Code)
	}
}

// TestServerIsHTTPHandler confirms the standalone Server can also be reused as
// an http.Handler, and that attaching the handler at Paths on a host mux serves
// every debug endpoint (the application-router embedding pattern).
func TestServerIsHTTPHandler(t *testing.T) {
	var h http.Handler = New(Config{Addr: "localhost:0", Liveness: okHandler(), Readiness: okHandler()})

	host := http.NewServeMux()
	for _, p := range Paths {
		host.Handle(p, h)
	}
	for _, path := range []string{"/debug/pprof/", "/healthz", "/readyz"} {
		rec := httptest.NewRecorder()
		host.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s via host mux = %d, want 200", path, rec.Code)
		}
	}
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
}

func TestNewBuildsServerWithDefaults(t *testing.T) {
	s := New(Config{Addr: "localhost:0"})
	if s.Server == nil {
		t.Fatal("expected an embedded httpserver.Server")
	}
	if s.Name() != "debug-server" {
		t.Fatalf("name = %q", s.Name())
	}
	if s.Addr() != "localhost:0" {
		t.Fatalf("addr = %q", s.Addr())
	}
}

func TestHandlerServesStartupAndVersion(t *testing.T) {
	h := Handler(Config{
		Startup: okHandler(),
		Version: map[string]string{"name": "svc", "version": "v1.2.3"},
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/startupz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/startupz = %d, want 200", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/version", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/version status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("/version content-type = %q, want application/json", ct)
	}

	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("/version body not JSON: %v", err)
	}
	if got["version"] != "v1.2.3" {
		t.Fatalf("/version body = %v", got)
	}
}

func TestHandlerOmitsUnsetStartupAndVersion(t *testing.T) {
	h := Handler(Config{})
	for _, path := range []string{"/startupz", "/version"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s with no handler = %d, want 404", path, rec.Code)
		}
	}
}
