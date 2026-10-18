package broker

import (
	"cmp"
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/assanoff/skit/internal/prom"
)

// The consumer metrics are named skit_broker_*.
const (
	metricsNamespace = "skit"
	metricsSubsystem = "broker"
)

// InstrumentConfig configures Instrument.
type InstrumentConfig struct {
	// Consumer names the consumer in the metrics (default "default").
	Consumer string
	// Metrics is the registry for the consumer metrics; nil — metrics off.
	Metrics prometheus.Registerer
}

// Instrument wraps h so every message it handles is recorded on cfg.Metrics:
//
//	skit_broker_consumed_total{consumer,action}   — messages by verdict
//	skit_broker_handle_duration_seconds{consumer} — duration of h
//
// action is "ack", "requeue", "discard", or "panic" when h panics; the panic
// goes on to the Guard of the consumer, which requeues the message. A verdict
// outside the three is recorded as "ack", as the adapters treat it. Wrap the
// handler you pass to an adapter or to Process, so a retried message is
// recorded on every attempt. With a nil cfg.Metrics, Instrument returns h.
func Instrument(h Handler, cfg InstrumentConfig) Handler {
	if cfg.Metrics == nil {
		return h
	}
	m := newConsumerMetrics(cfg.Metrics, cmp.Or(cfg.Consumer, "default"))
	return func(ctx context.Context, msg Message) Action {
		start := time.Now()
		action := Requeue
		// finished stays false when h panics: the deferred record then counts a
		// panic while the panic goes on to the Guard of the consumer.
		finished := false
		defer func() { m.record(ctx, time.Since(start), action, finished) }()
		action = h(ctx, msg)
		finished = true
		return action
	}
}

// consumerMetrics are the metrics of the messages of one consumer.
type consumerMetrics struct {
	ack      prometheus.Counter
	requeue  prometheus.Counter
	discard  prometheus.Counter
	panicked prometheus.Counter
	duration prometheus.Observer
}

// newConsumerMetrics builds the metrics of the consumer name and registers them
// on reg. The consumers on one registry share the metrics, each under its own
// name; the counters of every action start at 0.
func newConsumerMetrics(reg prometheus.Registerer, name string) *consumerMetrics {
	consumed := prom.MustRegister(reg, prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace, Subsystem: metricsSubsystem,
		Name: "consumed_total",
		Help: "Messages handled, by consumer and action: ack, requeue, discard or panic.",
	}, []string{"consumer", "action"}))
	duration := prom.MustRegister(reg, prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricsNamespace, Subsystem: metricsSubsystem,
		Name:    "handle_duration_seconds",
		Help:    "Duration of the message handler, by consumer.",
		Buckets: prom.DurationBuckets(),
	}, []string{"consumer"}))
	return &consumerMetrics{
		ack:      consumed.WithLabelValues(name, "ack"),
		requeue:  consumed.WithLabelValues(name, "requeue"),
		discard:  consumed.WithLabelValues(name, "discard"),
		panicked: consumed.WithLabelValues(name, "panic"),
		duration: duration.WithLabelValues(name),
	}
}

// record records a message handled in d: a panic when the handler did not
// finish, else its action.
func (m *consumerMetrics) record(ctx context.Context, d time.Duration, action Action, finished bool) {
	prom.ObserveDuration(ctx, m.duration, d)
	switch {
	case !finished:
		m.panicked.Inc()
	case action == Requeue:
		m.requeue.Inc()
	case action == Discard:
		m.discard.Inc()
	default:
		m.ack.Inc()
	}
}
