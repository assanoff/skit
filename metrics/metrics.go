package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/assanoff/skit/httpw"
)

// Metrics bundles a registry with the standard HTTP request collectors.
type Metrics struct {
	Registry  *prometheus.Registry
	reqTotal  *prometheus.CounterVec
	reqMillis *prometheus.HistogramVec
}

// New builds a Metrics with a fresh registry and registers Go runtime and
// process collectors plus HTTP request collectors.
func New(namespace string) *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	reqTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "http_requests_total",
		Help:      "Total number of HTTP requests.",
	}, []string{"method", "path", "status"})

	reqMillis := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "http_request_duration_ms",
		Help:      "HTTP request latency in milliseconds.",
		Buckets:   []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500},
	}, []string{"method", "path", "status"})

	reg.MustRegister(reqTotal, reqMillis)

	return &Metrics{Registry: reg, reqTotal: reqTotal, reqMillis: reqMillis}
}

// Handler returns the scrape handler for /metrics.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{})
}

// Middleware records count and latency for each request. routePattern should be
// the matched route template (low cardinality), not the raw path.
func (m *Metrics) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := httpw.Wrap(w)
			next.ServeHTTP(rec, r)

			pattern := r.Pattern
			if pattern == "" {
				// Unmatched routes / 404s have no pattern; using the raw path
				// here would let arbitrary URLs explode Prometheus label
				// cardinality (a memory-DoS vector).
				pattern = "<unmatched>"
			}
			// A handler that returns without touching the writer leaves Status
			// at 0; net/http sends 200 in that case, so record it as such.
			code := rec.Status()
			if code == 0 {
				code = http.StatusOK
			}
			status := strconv.Itoa(code)
			labels := prometheus.Labels{"method": r.Method, "path": pattern, "status": status}
			m.reqTotal.With(labels).Inc()
			m.reqMillis.With(labels).Observe(float64(time.Since(start).Milliseconds()))
		})
	}
}
