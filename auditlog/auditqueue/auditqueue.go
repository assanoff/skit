// Package auditqueue is a durable, queue-backed auditlog.Recorder: Record enqueues
// the audit entry onto a skit queue (one cheap INSERT on the request path)
// and a background Worker drains it into the audit core. Use it when audit loss on
// a crash is unacceptable or to smooth write spikes; for the lowest latency with
// strict per-model ordering use auditlog.AsyncRecorder instead.
//
// Ordering note: the queue does not guarantee per-model order across consumers.
// Core.Create retries on a version collision (so no row is lost), but if two
// changes to one model are processed out of order their stored version order can
// differ from real time. Run a single consumer, or use AsyncRecorder, when strict
// ordering matters.
package auditqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/assanoff/skit/auditlog"
	"github.com/assanoff/skit/auditlog/auditbus"
	"github.com/assanoff/skit/logger"
	"github.com/assanoff/skit/queue"
	"github.com/assanoff/skit/worker"
)

// Kind is the queue task kind for audit entries.
const Kind = "auditlog.record"

// Recorder enqueues audit entries onto a queue. It implements auditlog.Recorder.
type Recorder struct {
	q   queue.Queue
	log *logger.Logger
}

// NewRecorder builds a queue-backed recorder.
func NewRecorder(q queue.Queue, log *logger.Logger) *Recorder {
	return &Recorder{q: q, log: log}
}

// Record enqueues entry as a queue task (best-effort: a failed enqueue is logged,
// not returned). The Worker later writes it through the audit core.
func (r *Recorder) Record(ctx context.Context, entry auditlog.NewAuditLog) {
	payload, err := json.Marshal(entry.Payload)
	if err != nil {
		r.logErr(ctx, "marshal payload", entry, err)
		return
	}
	body, err := json.Marshal(auditbus.Event{
		ModelType: entry.ModelType,
		ModelID:   entry.ModelID,
		Method:    entry.Method,
		Path:      entry.Path,
		CreatedBy: entry.CreatedBy,
		Payload:   payload,
	})
	if err != nil {
		r.logErr(ctx, "marshal event", entry, err)
		return
	}
	if _, err := r.q.Schedule(ctx, queue.ScheduleParams{Kind: Kind, Payload: body}); err != nil {
		r.logErr(ctx, "enqueue", entry, err)
	}
}

func (r *Recorder) logErr(ctx context.Context, op string, entry auditlog.NewAuditLog, err error) {
	if r.log != nil {
		r.log.Error(ctx, "auditqueue: "+op+" failed",
			"model_type", entry.ModelType, "model_id", entry.ModelID, "err", err)
	}
}

// Worker drains audit tasks from the queue into the audit core.
type Worker struct {
	q     queue.Queue
	core  *auditlog.Core
	batch int
}

// WorkerConfig configures the queue-draining loop.
type WorkerConfig struct {
	// Interval between polls (required, > 0).
	Interval time.Duration
	// Batch caps tasks claimed per tick (default 100).
	Batch int
}

// NewWorker returns a worker.Runnable that claims audit tasks and records them.
// Supervise it in a worker.Group. log may be nil.
func NewWorker(q queue.Queue, core *auditlog.Core, log *logger.Logger, cfg WorkerConfig) *worker.Loop {
	w := &Worker{q: q, core: core, batch: cfg.Batch}
	if w.batch <= 0 {
		w.batch = 100
	}
	var sl *slog.Logger
	if log != nil {
		sl = log.Slog()
	}
	return worker.NewLoop(sl, worker.LoopConfig{
		Name:               "auditlog-queue",
		Interval:           cfg.Interval,
		ImmediateFirstTick: true,
	}, w.tick)
}

func (w *Worker) tick(ctx context.Context) error {
	tasks, err := w.q.Claim(ctx, time.Now(), w.batch)
	if err != nil {
		return fmt.Errorf("auditqueue: claim: %w", err)
	}
	for _, t := range tasks {
		var ev auditbus.Event
		if err := json.Unmarshal(t.Payload, &ev); err != nil {
			// Undecodable payload will never succeed; park it as a dead letter.
			_ = w.q.MarkFailed(ctx, t, "auditqueue: decode: "+err.Error(), true, time.Now())
			continue
		}
		if err := auditbus.Record(ctx, w.core, ev); err != nil {
			_ = w.q.MarkFailed(ctx, t, err.Error(), false, time.Now())
			continue
		}
		_ = w.q.MarkDone(ctx, t, time.Now())
	}
	return nil
}
