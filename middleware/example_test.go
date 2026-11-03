package middleware_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/assanoff/skit/middleware"
	"github.com/assanoff/skit/rest/router"
)

// Panics goes on the root, so it wraps every route; the guards go on the
// group of business routes, so the probe gets none of them.
func Example() {
	r := router.New()
	r.Use(middleware.Panics(nil))
	r.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	api := r.With(
		middleware.SecureHeaders(),
		middleware.RequestID(),
		middleware.SizeLimit(16),
	)
	api.HandleFunc("POST /notes", func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusCreated)
	})
	api.HandleFunc("GET /panic", func(http.ResponseWriter, *http.Request) {
		panic("nil map")
	})

	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/healthz", nil),
		httptest.NewRequest(http.MethodPost, "/notes", strings.NewReader("short")),
		httptest.NewRequest(http.MethodPost, "/notes", strings.NewReader(strings.Repeat("x", 64))),
		httptest.NewRequest(http.MethodGet, "/panic", nil),
	} {
		req.Header.Set(middleware.RequestIDHeader, "req-1")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		fmt.Printf("%s %s %d %q %q\n", req.Method, req.URL.Path, w.Code,
			w.Header().Get("X-Content-Type-Options"), w.Header().Get(middleware.RequestIDHeader))
	}
	// Output:
	// GET /healthz 200 "" ""
	// POST /notes 201 "nosniff" "req-1"
	// POST /notes 413 "nosniff" "req-1"
	// GET /panic 500 "nosniff" "req-1"
}

// CORS answers a preflight of an allowed origin with 204 and the
// Access-Control-* headers; another origin gets none, and the browser blocks it.
func ExampleCORS() {
	cors := middleware.CORS(middleware.CORSConfig{
		AllowedOrigins: []string{"https://app.example.com"},
		MaxAge:         600,
	})
	h := cors(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, origin := range []string{"https://app.example.com", "https://evil.example"} {
		req := httptest.NewRequest(http.MethodOptions, "/widgets", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", http.MethodPost)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		fmt.Printf("%d %q %q\n", w.Code,
			w.Header().Get("Access-Control-Allow-Origin"),
			w.Header().Get("Access-Control-Allow-Methods"))
	}
	// Output:
	// 204 "https://app.example.com" "GET, POST, PUT, PATCH, DELETE, OPTIONS"
	// 204 "" ""
}

// Route and Method are the labels of a request in the span, the access log and
// the metrics: the route template, not the path. A handler that routes by
// itself below a prefix names its route with SetRoute, which Trace keeps for
// the middleware inside it.
func ExampleRoute() {
	label := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
			fmt.Println(middleware.Method(r), middleware.Route(r))
		})
	}
	gateway := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		middleware.SetRoute(r, "/gw/v1/widgets/{id}")
	})

	r := router.New()
	api := r.With(middleware.Trace(middleware.TraceConfig{}), label)
	api.HandleFunc("GET /widgets/{id}", func(http.ResponseWriter, *http.Request) {
	})
	api.Handle("/gw/", gateway)

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/widgets/42", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("PURGE", "/gw/v1/widgets/7", nil))
	// Output:
	// GET /widgets/{id}
	// _OTHER /gw/v1/widgets/{id}
}
