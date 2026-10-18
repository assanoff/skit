package queue

import (
	"context"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/assanoff/skit/worker"
)

// Stats is a point-in-time snapshot of the queue table used for backlog
// metrics.
type Stats struct {
	Pending int64 // ready to claim: not leased, run_at reached
	Leased  int64 // claimed by a consumer, including expired leases
	Dead    int64 // dead letters awaiting Cleanup
}

// statsReader reads the backlog; *PG implements it.
type statsReader interface {
	Stats(ctx context.Context, now time.Time) (Stats, error)
}

// NewBacklog builds the worker that reads the backlog of pg every 30 seconds,
// the first time at start, and sets these gauges on reg:
//
//	skit_queue_pending — tasks ready to claim
//	skit_queue_leased  — tasks claimed by a consumer
//	skit_queue_dead    — dead letters awaiting Cleanup
//
// reg is required: the worker exists only for its gauges, so a process
// without metrics does not add it. A read that fails is logged and the gauges
// keep the values of the last read; how fresh they are tells
// skit_worker_last_success_timestamp_seconds{worker="queue-backlog"}.
// Every replica reads the whole table and reports the same values, so a query
// takes max without(pod, instance), not sum.
func NewBacklog(log *slog.Logger, pg *PG, reg prometheus.Registerer) *worker.Loop {
	const interval = 30 * time.Second
	b := newBacklog(pg, reg)
	return worker.NewLoop(log, worker.LoopConfig{
		Name:               "queue-backlog",
		Interval:           interval,
		ImmediateFirstTick: true,
		TickTimeout:        interval,
		Metrics:            reg,
	}, b.read)
}

// backlog sets the backlog gauges from the stats sr reads.
type backlog struct {
	sr      statsReader
	pending prometheus.Gauge
	leased  prometheus.Gauge
	dead    prometheus.Gauge
}

func newBacklog(sr statsReader, reg prometheus.Registerer) *backlog {
	return &backlog{
		sr: sr,
		pending: newGauge(reg, "pending",
			"Tasks ready to claim: not leased, run_at reached."),
		leased: newGauge(reg, "leased",
			"Tasks claimed by a consumer, including expired leases."),
		dead: newGauge(reg, "dead",
			"Dead letters awaiting Cleanup."),
	}
}

// read reads the backlog and sets the gauges; on an error it leaves them.
func (b *backlog) read(ctx context.Context) error {
	st, err := b.sr.Stats(ctx, time.Now().UTC())
	if err != nil {
		return err
	}
	b.pending.Set(float64(st.Pending))
	b.leased.Set(float64(st.Leased))
	b.dead.Set(float64(st.Dead))
	return nil
}
