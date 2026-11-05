package auditqueue

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/assanoff/skit/auditlog"
	"github.com/assanoff/skit/auditlog/auditbus"
	"github.com/assanoff/skit/queue"
	"github.com/assanoff/skit/worker"
)

const (
	// Queue is the queue the audit entries go through.
	Queue = "auditlog"
	// Kind is the queue task kind for audit entries.
	Kind = "auditlog.record"
)

// Enqueuer schedules a task on a queue; *queue.PG implements it.
type Enqueuer interface {
	Schedule(ctx context.Context, p queue.Params) (inserted bool, err error)
}

// Recorder enqueues audit entries onto a queue. It implements auditlog.Recorder.
type Recorder struct {
	q   Enqueuer
	log *slog.Logger
}

// NewRecorder builds a queue-backed recorder.
func NewRecorder(q Enqueuer, log *slog.Logger) *Recorder {
	return &Recorder{q: q, log: log}
}

// Record enqueues entry as a task of Queue (best-effort: a failed enqueue is
// logged, not returned). The workers later write it through the audit core.
func (r *Recorder) Record(ctx context.Context, entry auditlog.NewAuditLog) {
	payload, err := json.Marshal(entry.Payload, json.Deterministic(true))
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
	}, json.Deterministic(true))
	if err != nil {
		r.logErr(ctx, "marshal event", entry, err)
		return
	}
	p := queue.Params{Queue: Queue, Kind: Kind, Payload: body}
	if _, err := r.q.Schedule(ctx, p); err != nil {
		r.logErr(ctx, "enqueue", entry, err)
	}
}

func (r *Recorder) logErr(ctx context.Context, op string, entry auditlog.NewAuditLog, err error) {
	if r.log != nil {
		r.log.ErrorContext(ctx, "auditqueue: "+op+" failed",
			"model_type", entry.ModelType, "model_id", entry.ModelID, "err", err)
	}
}

// WorkerConfig configures the workers of the audit queue.
type WorkerConfig struct {
	// Interval is the wait after a poll that found no entry (default 1s).
	Interval time.Duration
	// Batch caps the entries claimed at once (default 100).
	Batch int
	// Metrics is the registry for the skit_queue_* metrics of the audit queue
	// and the skit_worker_* metrics of its loops; nil — metrics off.
	Metrics prometheus.Registerer
}

// NewWorkers returns the workers of Queue (queue.Workers), which record the
// audit entries through core. A payload that does not decode goes to the dead
// letters; a failed record is retried with backoff. Supervise the workers in
// a worker.Group. log may be nil.
//
// It returns an error only if wiring the queue fails — which cannot happen
// here, as the single registration uses a constant Kind and a non-nil handler.
func NewWorkers(q *queue.PG, core *auditlog.Core, log *slog.Logger, cfg WorkerConfig) ([]worker.Runnable, error) {
	mux := queue.NewMux()
	if err := mux.Register(Kind, queue.Decode(recordTo(core))); err != nil {
		return nil, fmt.Errorf("auditqueue: new workers: %w", err)
	}
	return queue.Workers(log, q, queue.Config{
		Queue: Queue,
		Batch: cmp.Or(cfg.Batch, 100),
		Poll:  cfg.Interval,
	}, mux.Handle, queue.WithMetrics(cfg.Metrics))
}

// recordTo returns the handler that records an audit entry through core.
func recordTo(core *auditlog.Core) func(context.Context, queue.Task, auditbus.Event) error {
	return func(ctx context.Context, _ queue.Task, ev auditbus.Event) error {
		if err := auditbus.Record(ctx, core, ev); err != nil {
			return fmt.Errorf("auditqueue: record: %w", err)
		}
		return nil
	}
}
