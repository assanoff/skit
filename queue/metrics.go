package queue

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/assanoff/skit/internal/prom"
)

// The queue metrics are named skit_queue_*. A task with a kind no handler is
// registered for is recorded under kindFallback, when the Mux has a fallback,
// or kindUnknown.
const (
	metricsNamespace = "skit"
	metricsSubsystem = "queue"

	kindFallback = "<fallback>"
	kindUnknown  = "<unknown>"
)

// muxMetrics are the metrics of the tasks a Mux handles.
type muxMetrics struct {
	handled  *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// newMuxMetrics builds the metrics of a Mux and registers them on reg; the
// muxes on one registry share them. With a nil reg the metrics are recorded
// but not exposed.
func newMuxMetrics(reg prometheus.Registerer) *muxMetrics {
	return &muxMetrics{
		handled: prom.MustRegister(reg, prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: metricsSubsystem,
			Name: "handled_total",
			Help: "Tasks handled, by kind and result: ok or error.",
		}, []string{"kind", "result"})),
		duration: prom.MustRegister(reg, prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace, Subsystem: metricsSubsystem,
			Name:    "handle_duration_seconds",
			Help:    "Duration of the task handler, by kind.",
			Buckets: prom.DurationBuckets(),
		}, []string{"kind"})),
	}
}

// kind binds the metrics of kind; its counters start at 0.
func (m *muxMetrics) kind(kind string) kindMetrics {
	return kindMetrics{
		ok:       m.handled.WithLabelValues(kind, "ok"),
		failed:   m.handled.WithLabelValues(kind, "error"),
		duration: m.duration.WithLabelValues(kind),
	}
}

// kindMetrics are the metrics of the tasks of one kind.
type kindMetrics struct {
	ok       prometheus.Counter
	failed   prometheus.Counter
	duration prometheus.Observer
}

// observe records a task handled in d with err.
func (k kindMetrics) observe(ctx context.Context, d time.Duration, err error) {
	prom.ObserveDuration(ctx, k.duration, d)
	if err != nil {
		k.failed.Inc()
		return
	}
	k.ok.Inc()
}

// newGauge builds the gauge skit_queue_<name> and registers it on reg.
func newGauge(reg prometheus.Registerer, name, help string) prometheus.Gauge {
	return prom.MustRegister(reg, prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: metricsNamespace, Subsystem: metricsSubsystem,
		Name: name,
		Help: help,
	}))
}
