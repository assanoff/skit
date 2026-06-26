package queue

import (
	"context"
	"errors"
	"time"
)

// ErrLeaseLost is returned by MarkDone/MarkFailed when the task's lease no longer
// matches — another consumer reclaimed it after the lease expired. The mark is a
// no-op; the other consumer now owns the task.
var ErrLeaseLost = errors.New("queue: lease lost")

// Task is a unit of queued work — a pure core type with no db tags (the PG
// store owns that mapping via taskRow). Payload is opaque to the queue;
// consumers route on Kind and decode Payload (typically JSON) themselves.
type Task struct {
	ID        int64
	Name      string
	Kind      string
	Payload   []byte
	CreatedAt time.Time
	RunAt     time.Time
	Attempts  int
	LeaseID   string
	LastError string
}

// ScheduleParams describes a task to enqueue.
type ScheduleParams struct {
	// Name is an optional dedup key. When set, scheduling the same Name twice is a
	// no-op (Schedule reports inserted=false). When empty, the queue assigns a
	// unique name so every call enqueues a distinct task.
	Name string
	// Kind routes the task to a handler; required.
	Kind string
	// Payload is the opaque task body.
	Payload []byte
	// Delay postpones the earliest claim time (RunAt = now + Delay). Zero means
	// claimable immediately.
	Delay time.Duration
}

// Queue is a durable work queue. Implementations are safe for concurrent use by
// multiple consumers. Claim/MarkDone/MarkFailed mirror worker.Source/Sink.
type Queue interface {
	// Schedule enqueues a task, returning inserted=false if Name already exists.
	Schedule(ctx context.Context, p ScheduleParams) (inserted bool, err error)
	// Claim leases up to limit ready tasks (run_at <= now and not currently
	// leased), bumping their attempt count. Each returned Task carries a fresh
	// LeaseID that MarkDone/MarkFailed must echo back.
	Claim(ctx context.Context, now time.Time, limit int) ([]Task, error)
	// MarkDone acknowledges successful processing and removes the task.
	MarkDone(ctx context.Context, t Task, now time.Time) error
	// MarkFailed records a failure. terminal=true parks the task as a dead letter;
	// terminal=false releases the lease so a later Claim retries it.
	MarkFailed(ctx context.Context, t Task, errMsg string, terminal bool, now time.Time) error
}

// Schema returns the DDL that creates the queue_tasks table and its index. Use
// it in a migration, or call PG.EnsureSchema in tests.
func Schema() string {
	return `
CREATE TABLE IF NOT EXISTS queue_tasks (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    kind       TEXT NOT NULL,
    payload    BYTEA,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    run_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    attempts   INT NOT NULL DEFAULT 0,
    leased_at  TIMESTAMPTZ,
    lease_id   TEXT,
    last_error TEXT,
    done_at    TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS queue_tasks_ready_idx ON queue_tasks (done_at, run_at);`
}
