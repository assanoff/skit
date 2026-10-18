package outbox

import (
	"context"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/assanoff/skit/worker"
)

// Stats is a point-in-time snapshot of the outbox table used for backlog
// metrics: how much work is waiting and how old the oldest waiting event is.
type Stats struct {
	Pending          int64
	InFlight         int64
	Failed           int64
	OldestPendingAge time.Duration // age of the oldest pending row; 0 when none
}

// StatsReader reports outbox backlog stats. *PG implements it; it is a separate
// capability from Store so the core port stays minimal and backlog metrics stay
// opt-in.
type StatsReader interface {
	Stats(ctx context.Context, now time.Time) (Stats, error)
}

// NewBacklog builds the worker that reads the outbox backlog through sr every
// 30 seconds, the first time at start, and sets these gauges on metrics:
//
//	skit_outbox_pending                — events awaiting the relay
//	skit_outbox_in_flight              — events leased, publish in progress
//	skit_outbox_failed                 — events in the terminal failed state
//	skit_outbox_oldest_pending_seconds — age of the oldest pending event
//
// metrics is required: the worker exists only for its gauges, so a process
// without metrics does not add it. A read that fails is logged and the gauges
// keep the values of the last read. Every replica reads the whole table and
// reports the same values, so a query takes max without(pod, instance), not
// sum.
func NewBacklog(log *slog.Logger, sr StatsReader, metrics prometheus.Registerer) *worker.Loop {
	const interval = 30 * time.Second
	b := newBacklog(sr, metrics)
	return worker.NewLoop(log, worker.LoopConfig{
		Name:               "outbox-backlog",
		Interval:           interval,
		ImmediateFirstTick: true,
		TickTimeout:        interval,
	}, b.read)
}

// backlog sets the backlog gauges from the stats sr reads.
type backlog struct {
	sr       StatsReader
	pending  prometheus.Gauge
	inFlight prometheus.Gauge
	failed   prometheus.Gauge
	oldest   prometheus.Gauge
}

func newBacklog(sr StatsReader, metrics prometheus.Registerer) *backlog {
	return &backlog{
		sr: sr,
		pending: newGauge(metrics, "pending",
			"Events awaiting the relay (status=pending)."),
		inFlight: newGauge(metrics, "in_flight",
			"Events leased by a relay, publish in progress."),
		failed: newGauge(metrics, "failed",
			"Events in the terminal failed state."),
		oldest: newGauge(metrics, "oldest_pending_seconds",
			"Age in seconds of the oldest pending event (0 when none)."),
	}
}

// read reads the backlog and sets the gauges; on an error it leaves them.
func (b *backlog) read(ctx context.Context) error {
	st, err := b.sr.Stats(ctx, time.Now().UTC())
	if err != nil {
		return err
	}
	b.pending.Set(float64(st.Pending))
	b.inFlight.Set(float64(st.InFlight))
	b.failed.Set(float64(st.Failed))
	b.oldest.Set(st.OldestPendingAge.Seconds())
	return nil
}
