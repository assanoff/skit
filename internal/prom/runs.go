package prom

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// namespace prefixes the names of the SDK metrics.
const namespace = "skit"

// RunsOpts names the metrics of Runs:
//   - skit_<Subsystem>_<Run>s_total by Label and result;
//   - skit_<Subsystem>_<Run>_duration_seconds by Label;
//   - skit_<Subsystem>_last_success_timestamp_seconds by Label.
type RunsOpts struct {
	Subsystem string // "worker"
	Run       string // "tick"
	Label     string // "worker"
}

// Runs records the runs of one periodic unit of work, such as a worker tick or
// a cron job: the runs by result (ok, error, panic), their duration and the
// time of the last success.
type Runs struct {
	name        string
	ok          prometheus.Counter
	failed      prometheus.Counter
	panicked    prometheus.Counter
	duration    prometheus.Observer
	lastSuccess *prometheus.GaugeVec
}

// NewRuns builds the metrics of the unit name and registers them on reg; the
// units of one kind share the metrics, each under its own name. The counters
// start at 0, the last success appears with the first one.
func NewRuns(reg prometheus.Registerer, opts RunsOpts, name string) *Runs {
	runs := MustRegister(reg, prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Subsystem: opts.Subsystem,
		Name: opts.Run + "s_total",
		Help: "Runs by " + opts.Label + " and result: ok, error or panic.",
	}, []string{opts.Label, "result"}))
	duration := MustRegister(reg, prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace, Subsystem: opts.Subsystem,
		Name:    opts.Run + "_duration_seconds",
		Help:    "Duration of runs, by " + opts.Label + ".",
		Buckets: DurationBuckets(),
	}, []string{opts.Label}))
	lastSuccess := MustRegister(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace, Subsystem: opts.Subsystem,
		Name: "last_success_timestamp_seconds",
		Help: "Unix time of the last run that ended without an error, by " + opts.Label + ".",
	}, []string{opts.Label}))
	return &Runs{
		name:        name,
		ok:          runs.WithLabelValues(name, "ok"),
		failed:      runs.WithLabelValues(name, "error"),
		panicked:    runs.WithLabelValues(name, "panic"),
		duration:    duration.WithLabelValues(name),
		lastSuccess: lastSuccess,
	}
}

// Record records a run that took d: a panic when it did not finish, else an
// error or a success by err.
func (r *Runs) Record(ctx context.Context, d time.Duration, finished bool, err error) {
	ObserveDuration(ctx, r.duration, d)
	switch {
	case !finished:
		r.panicked.Inc()
	case err != nil:
		r.failed.Inc()
	default:
		r.ok.Inc()
		r.lastSuccess.WithLabelValues(r.name).SetToCurrentTime()
	}
}
