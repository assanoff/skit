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
	"errors"
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

// errBadEvent marks a permanently undecodable task payload. It is classified as
// terminal so the processor dead-letters the task instead of retrying it.
var errBadEvent = errors.New("auditqueue: undecodable event")

// Worker records claimed audit tasks into the audit core.
type Worker struct {
	core *auditlog.Core
}

// WorkerConfig configures the queue-draining loop.
type WorkerConfig struct {
	// Interval between polls (required, > 0).
	Interval time.Duration
	// Batch caps tasks claimed per tick (default 100).
	Batch int
}

// NewWorker returns a worker.Loop that claims audit tasks and records them via
// the audit core. It drives a worker.Processor (q is both Source and Sink) with
// a queue.Mux dispatching the audit Kind, so the claim -> handle -> ack/retry
// loop, retry scheduling, and dead-lettering are shared with every other queue
// consumer instead of hand-rolled. Supervise the returned Loop in a
// worker.Group. log may be nil.
//
// It returns an error only if wiring the Mux fails — which cannot happen here,
// as the single registration uses a constant Kind and a non-nil handler.
func NewWorker(q queue.Queue, core *auditlog.Core, log *logger.Logger, cfg WorkerConfig) (*worker.Loop, error) {
	batch := cfg.Batch
	if batch <= 0 {
		batch = 100
	}
	var sl *slog.Logger
	if log != nil {
		sl = log.Slog()
	}

	w := &Worker{core: core}
	mux := queue.NewMux()
	if err := mux.Register(Kind, w.handle); err != nil {
		return nil, fmt.Errorf("auditqueue: new worker: %w", err)
	}

	proc := worker.NewProcessor[queue.Task](sl, q, mux, q, worker.ProcessorConfig{
		Name:       "auditlog-queue",
		BatchSize:  batch,
		IsTerminal: func(err error) bool { return errors.Is(err, errBadEvent) },
	})
	return worker.NewLoop(sl, worker.LoopConfig{
		Name:               "auditlog-queue",
		Interval:           cfg.Interval,
		ImmediateFirstTick: true,
	}, proc.Tick()), nil
}

// handle records one audit task. An undecodable payload is terminal (it will
// never parse); a record failure is returned as-is so the queue retries it.
func (w *Worker) handle(ctx context.Context, t queue.Task) error {
	var ev auditbus.Event
	if err := json.Unmarshal(t.Payload, &ev); err != nil {
		return fmt.Errorf("%w: %w", errBadEvent, err)
	}
	if err := auditbus.Record(ctx, w.core, ev); err != nil {
		return fmt.Errorf("auditqueue: record: %w", err)
	}
	return nil
}
