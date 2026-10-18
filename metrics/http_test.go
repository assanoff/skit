package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matryer/is"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/assanoff/skit/middleware"
)

func TestMiddlewareRecordsRequest(t *testing.T) {
	is := is.New(t)
	m := New()
	mux := http.NewServeMux()
	mux.Handle("GET /items/{id}", m.Middleware()(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			is.Equal(testutil.ToFloat64(m.http.inFlight), 1.0) // counted while served
			w.WriteHeader(http.StatusCreated)
		})))

	serve(t, mux, http.MethodGet, "/items/7")

	is.Equal(requests(m, "GET", "/items/{id}", "201"), 1.0)
	is.Equal(durations(t, m, "GET", "/items/{id}"), uint64(1))
	is.Equal(testutil.ToFloat64(m.http.inFlight), 0.0)
}

func TestMiddlewareStatus(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{
			name:    "no header written",
			handler: emptyHandler,
			want:    "200",
		},
		{
			name: "error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			want: "404",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)
			m := New()
			h := m.Middleware()(tt.handler)

			serve(t, h, http.MethodGet, "/")

			is.Equal(requests(m, "GET", "", tt.want), 1.0)
			is.Equal(testutil.ToFloat64(m.http.inFlight), 0.0)
		})
	}
}

// A panicking handler is recorded with status 500, and the panic goes on to the
// recoverer above Middleware.
func TestMiddlewarePanic(t *testing.T) {
	is := is.New(t)
	m := New()
	h := m.Middleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		serve(t, h, http.MethodGet, "/")
	}()

	is.Equal(recovered, "boom") // Middleware re-raises the panic of the handler
	is.Equal(requests(m, "GET", "", "500"), 1.0)
	is.Equal(testutil.ToFloat64(m.http.inFlight), 0.0)
}

func TestMiddlewareMethod(t *testing.T) {
	tests := []struct{ method, want string }{
		{method: "get", want: "GET"},
		{method: "PROPFIND", want: "_OTHER"},
		{method: "QUERY", want: "_OTHER"},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			is := is.New(t)
			m := New()
			h := m.Middleware()(http.HandlerFunc(emptyHandler))

			serve(t, h, tt.method, "/")

			is.Equal(requests(m, tt.want, "", "200"), 1.0)
		})
	}
}

// A handler that routes by itself below the route group, such as a gateway
// mounted on a prefix, labels the request with the route it set.
func TestMiddlewareRouteFromSetRoute(t *testing.T) {
	is := is.New(t)
	m := New()
	traced := middleware.Trace(middleware.TraceConfig{
		TracerProvider: tracenoop.NewTracerProvider(),
	})
	mux := http.NewServeMux()
	mux.Handle("/v1/", traced(m.Middleware()(http.HandlerFunc(
		func(_ http.ResponseWriter, r *http.Request) {
			middleware.SetRoute(r, "/v1/widgets/{id}")
		}))))

	serve(t, mux, http.MethodGet, "/v1/widgets/7")

	is.Equal(requests(m, "GET", "/v1/widgets/{id}", "200"), 1.0)
	is.Equal(requests(m, "GET", "/v1/", "200"), 0.0)
}

func TestHandlerExemplar(t *testing.T) {
	tests := []struct {
		name  string
		flags trace.TraceFlags
		want  bool
	}{
		{name: "sampled", flags: trace.FlagsSampled, want: true},
		{name: "not sampled", flags: 0, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)
			m := New()
			sc := trace.NewSpanContext(trace.SpanContextConfig{
				TraceID:    trace.TraceID{0xa},
				SpanID:     trace.SpanID{0xb},
				TraceFlags: tt.flags,
			})
			h := withSpanContext(sc, m.Middleware()(http.HandlerFunc(emptyHandler)))

			serve(t, h, http.MethodGet, "/")
			body := scrape(t, m).Body.String()

			want := `trace_id="` + sc.TraceID().String() + `"`
			is.Equal(strings.Contains(body, want), tt.want)
		})
	}
}

func TestHandlerServesOpenMetrics(t *testing.T) {
	is := is.New(t)

	w := scrape(t, New())

	is.Equal(w.Code, http.StatusOK)
	ct := w.Header().Get("Content-Type")
	is.True(strings.HasPrefix(ct, "application/openmetrics-text")) // Handler negotiates OpenMetrics
}

func TestHandlerBasePreset(t *testing.T) {
	is := is.New(t)

	body := scrape(t, New()).Body.String()

	is.True(strings.Contains(body, "go_build_info{"))
	is.True(strings.Contains(body, "go_goroutines "))
	is.True(strings.Contains(body, "skit_http_server_requests_in_flight "))
}

// emptyHandler returns without writing, as a handler of 200 with no body does.
func emptyHandler(http.ResponseWriter, *http.Request) {}

func serve(t *testing.T, h http.Handler, method, target string) {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), method, target, http.NoBody)
	h.ServeHTTP(httptest.NewRecorder(), r)
}

func withSpanContext(sc trace.SpanContext, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := trace.ContextWithSpanContext(r.Context(), sc)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// scrape reads /metrics as Prometheus does, asking for OpenMetrics.
func scrape(t *testing.T, m *Metrics) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", http.NoBody)
	r.Header.Set("Accept", "application/openmetrics-text; version=1.0.0")
	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, r)
	return w
}

func requests(m *Metrics, method, route, code string) float64 {
	return testutil.ToFloat64(m.http.requests.WithLabelValues(method, route, code))
}

// durations is the number of durations recorded for method and route.
func durations(t *testing.T, m *Metrics, method, route string) uint64 {
	t.Helper()
	var d dto.Metric
	h := m.http.duration.WithLabelValues(method, route).(prometheus.Histogram)
	is.New(t).NoErr(h.Write(&d))
	return d.GetHistogram().GetSampleCount()
}
