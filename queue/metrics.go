package queue

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/assanoff/skit/internal/prom"
)

// The queue metrics are named skit_queue_* and labeled with the queue.
const (
	metricsNamespace = "skit"
	metricsSubsystem = "queue"
	labelQueue       = "queue"
)

// taskMetrics are the metrics of the tasks the workers of one queue handle.
type taskMetrics struct {
	queue    string
	handled  *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// newTaskMetrics builds the task metrics of queue and registers them on reg;
// the queues on one registry share them. With a nil reg the metrics are
// recorded but not exposed.
func newTaskMetrics(reg prometheus.Registerer, queue string) *taskMetrics {
	return &taskMetrics{
		queue: queue,
		handled: prom.MustRegister(reg, prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: metricsSubsystem,
			Name: "handled_total",
			Help: "Tasks handled, by queue, kind and result: ok or error.",
		}, []string{labelQueue, "kind", "result"})),
		duration: prom.MustRegister(reg, prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace, Subsystem: metricsSubsystem,
			Name:    "handle_duration_seconds",
			Help:    "Duration of the task handler, by queue and kind.",
			Buckets: prom.DurationBuckets(),
		}, []string{labelQueue, "kind"})),
	}
}

// observe records a task of kind handled in d with err; a panic is an error.
func (m *taskMetrics) observe(ctx context.Context, kind string, d time.Duration, err error) {
	prom.ObserveDuration(ctx, m.duration.WithLabelValues(m.queue, kind), d)
	result := "ok"
	if err != nil {
		result = "error"
	}
	m.handled.WithLabelValues(m.queue, kind, result).Inc()
}

// newGauge builds the gauge skit_queue_<name> of queue and registers its vec
// on reg.
func newGauge(reg prometheus.Registerer, queue, name, help string) prometheus.Gauge {
	vec := prom.MustRegister(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace, Subsystem: metricsSubsystem,
		Name: name,
		Help: help,
	}, []string{labelQueue}))
	return vec.WithLabelValues(queue)
}
