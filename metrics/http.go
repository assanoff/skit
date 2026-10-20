package metrics

import (
	"cmp"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/assanoff/skit/internal/httpw"
	"github.com/assanoff/skit/internal/prom"
	"github.com/assanoff/skit/middleware"
)

// The HTTP server metrics are named skit_http_server_*. Their labels are named
// after the attributes of the HTTP semantic conventions, as the span names them.
const (
	namespace     = "skit"
	httpSubsystem = "http_server"

	labelMethod = "http_request_method"
	labelRoute  = "http_route"
	labelStatus = "http_response_status_code"
)

// httpMetrics are the HTTP server metrics Middleware records.
type httpMetrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inFlight prometheus.Gauge
}

func newHTTPMetrics(reg prometheus.Registerer) httpMetrics {
	f := promauto.With(reg)
	return httpMetrics{
		requests: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Subsystem: httpSubsystem,
			Name: "requests_total",
			Help: "HTTP requests served, by method, route and status code.",
		}, []string{labelMethod, labelRoute, labelStatus}),
		duration: f.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace, Subsystem: httpSubsystem,
			Name:    "request_duration_seconds",
			Help:    "Duration of HTTP requests, by method and route.",
			Buckets: prom.DurationBuckets(),
		}, []string{labelMethod, labelRoute}),
		inFlight: f.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace, Subsystem: httpSubsystem,
			Name: "requests_in_flight",
			Help: "HTTP requests being served.",
		}),
	}
}

// Middleware records every request it wraps:
//   - skit_http_server_requests_total by method, route and status code;
//   - skit_http_server_request_duration_seconds by method and route;
//   - skit_http_server_requests_in_flight.
//
// Put it into the group of business routes, after middleware.Trace. The route
// is the one middleware.Route reports — the route of middleware.SetRoute, else
// r.Pattern — and the duration of a sampled request carries its trace as the
// exemplar. Outside a route group r.Pattern is empty, and so is the route.
// A method the HTTP semantic conventions do not know is recorded as _OTHER.
//
// A request whose handler panics is recorded with status 500; the panic goes on
// to the recoverer above.
func (m *Metrics) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := httpw.Wrap(w)
			m.http.inFlight.Inc()
			done := false
			defer func() {
				m.http.inFlight.Dec()
				m.http.record(r, status(rec.Status(), done), time.Since(start))
			}()
			next.ServeHTTP(rec, r)
			done = true
		})
	}
}

func (h *httpMetrics) record(r *http.Request, code int, d time.Duration) {
	method, route := middleware.Method(r), middleware.Route(r)
	h.requests.WithLabelValues(method, route, strconv.Itoa(code)).Inc()
	prom.ObserveDuration(r.Context(), h.duration.WithLabelValues(method, route), d)
}

// status is the status code a request is recorded with: 500 when the handler
// panicked, 200 when it returned without writing a header (net/http sends 200
// then).
func status(written int, done bool) int {
	if !done {
		return http.StatusInternalServerError
	}
	return cmp.Or(written, http.StatusOK)
}
