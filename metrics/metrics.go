package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics is the registry of the process. The embedded *prometheus.Registry
// makes it a prometheus.Registerer: pass m itself to the Metrics field of a
// component's Config.
type Metrics struct {
	*prometheus.Registry

	http httpMetrics
}

// New builds the registry of the process with the base preset — go_*,
// process_* and go_build_info — and the HTTP server metrics that Middleware
// records. Build it once, in the Deps literal, before the initializers.
func New() *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewBuildInfoCollector(),
	)
	return &Metrics{Registry: reg, http: newHTTPMetrics(reg)}
}

// Handler serves the registry for scraping. A scraper that accepts
// OpenMetrics, as Prometheus does, also gets the exemplars; Prometheus stores
// them with --enable-feature=exemplar-storage.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{EnableOpenMetrics: true})
}
