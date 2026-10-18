package outbox

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/assanoff/skit/internal/prom"
)

// The outbox metrics are named skit_outbox_*. A constructor registers its
// metrics on the registry of its config; constructors on the same registry
// share them. With a nil registry the metrics are recorded but not exposed.
const (
	metricsNamespace = "skit"
	metricsSubsystem = "outbox"
)

// relayMetrics are the metrics of the relay's publish calls.
type relayMetrics struct {
	published prometheus.Counter
	failed    prometheus.Counter
	duration  prometheus.Histogram
}

func newRelayMetrics(metrics prometheus.Registerer) *relayMetrics {
	return &relayMetrics{
		published: newCounter(metrics, "relay_published_total",
			"Events the relay published to the broker successfully."),
		failed: newCounter(metrics, "relay_publish_failures_total",
			"Relay publish attempts that returned an error."),
		duration: prom.MustRegister(metrics, prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: metricsNamespace, Subsystem: metricsSubsystem,
			Name:    "relay_publish_duration_seconds",
			Help:    "Duration of a successful relay publish call.",
			Buckets: prom.DurationBuckets(),
		})),
	}
}

// observe records a publish call that ended with err after d.
func (m *relayMetrics) observe(ctx context.Context, d time.Duration, err error) {
	if err != nil {
		m.failed.Inc()
		return
	}
	m.published.Inc()
	prom.ObserveDuration(ctx, m.duration, d)
}

// newCounter builds the counter skit_outbox_<name> and registers it on metrics.
func newCounter(metrics prometheus.Registerer, name, help string) prometheus.Counter {
	return prom.MustRegister(metrics, prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace, Subsystem: metricsSubsystem,
		Name: name,
		Help: help,
	}))
}

// newGauge builds the gauge skit_outbox_<name> and registers it on metrics.
func newGauge(metrics prometheus.Registerer, name, help string) prometheus.Gauge {
	return prom.MustRegister(metrics, prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: metricsNamespace, Subsystem: metricsSubsystem,
		Name: name,
		Help: help,
	}))
}

// addRows adds to c the n rows a tick changed.
func addRows(c prometheus.Counter, n int64) {
	if n > 0 {
		c.Add(float64(n))
	}
}
