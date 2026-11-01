package queue

import (
	"context"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/assanoff/skit/worker"
)

// backlogInterval is how often the backlog of a queue is read.
const backlogInterval = 30 * time.Second

// statsReader reads the backlog of a queue; *PG implements it.
type statsReader interface {
	stats(ctx context.Context, queue string, now time.Time) (stats, error)
}

// newBacklogLoop builds the loop that reads the backlog of queue every 30
// seconds, the first time at start, into the gauges skit_queue_pending,
// skit_queue_leased and skit_queue_dead with the label queue. A read that
// fails is logged and the gauges keep the values of the last read. Every
// replica reads the same table and reports the same values, so a query takes
// max without(pod, instance), not sum.
func newBacklogLoop(log *slog.Logger, sr statsReader, queue string, reg prometheus.Registerer) *worker.Loop {
	b := newBacklog(sr, queue, reg)
	return worker.NewLoop(log, worker.LoopConfig{
		Name:               "queue-" + queue + "-backlog",
		Interval:           backlogInterval,
		ImmediateFirstTick: true,
		TickTimeout:        backlogInterval,
		Metrics:            reg,
	}, b.read)
}

// backlog sets the backlog gauges of a queue from the stats sr reads.
type backlog struct {
	sr      statsReader
	queue   string
	pending prometheus.Gauge
	leased  prometheus.Gauge
	dead    prometheus.Gauge
}

func newBacklog(sr statsReader, queue string, reg prometheus.Registerer) *backlog {
	return &backlog{
		sr:    sr,
		queue: queue,
		pending: newGauge(reg, queue, "pending",
			"Tasks ready to claim: not leased, run_at reached."),
		leased: newGauge(reg, queue, "leased",
			"Tasks claimed by a consumer, including expired leases."),
		dead: newGauge(reg, queue, "dead",
			"Dead letters awaiting the cleaner."),
	}
}

// read reads the backlog and sets the gauges; on an error it leaves them.
func (b *backlog) read(ctx context.Context) error {
	st, err := b.sr.stats(ctx, b.queue, time.Now().UTC())
	if err != nil {
		return err
	}
	b.pending.Set(float64(st.Pending))
	b.leased.Set(float64(st.Leased))
	b.dead.Set(float64(st.Dead))
	return nil
}
