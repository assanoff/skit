package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/logger"
	"github.com/assanoff/skit/worker"
)

// PG is a Postgres-backed Queue. Every query is an inline const right next to
// the method that runs it — no string-templated query cache, no fmt.Sprintf —
// so the SQL reads as SQL. The backing table is fixed (queue_tasks), and all
// time math (lease cutoff) and id generation (lease_id) happen in Go, so the
// SQL stays free of NOW()/gen_random_uuid()/interval casts.
//
// PG is safe for concurrent use by multiple goroutines and processes; Claim
// coordinates consumers via FOR UPDATE SKIP LOCKED so each ready task is handed
// to exactly one consumer at a time.
type PG struct {
	log          *logger.Logger
	db           *sqlx.DB
	leaseTimeout time.Duration
	retryDelay   time.Duration
}

// Compile-time checks: PG is a Queue and plugs into worker.Processor.
var (
	_ Queue               = (*PG)(nil)
	_ worker.Source[Task] = (*PG)(nil)
	_ worker.Sink[Task]   = (*PG)(nil)
)

// Options configures a queue (PG and InMem share it).
type Options struct {
	// LeaseTimeout is how long a claimed task stays leased before another
	// consumer may reclaim it, guarding against a consumer that died mid-process
	// (default 5m).
	LeaseTimeout time.Duration
	// RetryDelay is how long a retryable (non-terminal) failure waits before the
	// task becomes claimable again — it reschedules run_at to now+RetryDelay
	// instead of retrying immediately, so a persistently failing task does not
	// busy-loop (default 30s). Permanent failures (terminal=true) are
	// dead-lettered regardless. For fast in-process retries of transient errors,
	// wrap the handler with Retry; this delay governs the durable store retry.
	RetryDelay time.Duration
}

// NewPG builds a Postgres queue. It does not create the table; run a migration
// with Schema() or call EnsureSchema (handy in tests).
func NewPG(log *logger.Logger, db *sqlx.DB, opts Options) *PG {
	lease := opts.LeaseTimeout
	if lease <= 0 {
		lease = 5 * time.Minute
	}
	retryDelay := opts.RetryDelay
	if retryDelay <= 0 {
		retryDelay = 30 * time.Second
	}
	return &PG{log: log, db: db, leaseTimeout: lease, retryDelay: retryDelay}
}

// schemaLockKey keys the transaction-level advisory lock that serializes
// EnsureSchema across replicas booting at once.
var schemaLockKey = dbx.AdvisoryKey("skit.queue.schema")

// EnsureSchema creates the backing table and index if they do not exist. It is
// safe to call at startup, including from several replicas at once: the DDL runs
// under a transaction-scoped advisory lock, so concurrent boots serialize
// instead of racing on CREATE TABLE.
func (q *PG) EnsureSchema(ctx context.Context) error {
	return dbx.EnsureSchema(ctx, q.log, q.db, schemaLockKey, Schema())
}

// Schedule enqueues a task. An empty Name is replaced with a unique value so the
// call always inserts.
func (q *PG) Schedule(ctx context.Context, p ScheduleParams) (bool, error) {
	const query = `
INSERT INTO queue_tasks (name, kind, payload, run_at)
VALUES (:name, :kind, :payload, :run_at)
ON CONFLICT (name) DO NOTHING`
	name := p.Name
	if name == "" {
		name = uuid.NewString()
	}
	args := struct {
		Name    string    `db:"name"`
		Kind    string    `db:"kind"`
		Payload []byte    `db:"payload"`
		RunAt   time.Time `db:"run_at"`
	}{Name: name, Kind: p.Kind, Payload: p.Payload, RunAt: time.Now().UTC().Add(p.Delay)}

	n, err := dbx.NamedExecContextRowsAffected(ctx, q.log, q.db, query, args)
	if err != nil {
		return false, fmt.Errorf("queue schedule: %w", err)
	}
	return n == 1, nil
}

// Claim atomically leases up to limit ready tasks (run_at <= now, not currently
// leased or lease expired) under a fresh lease id, bumping their attempt count.
// A single CTE + UPDATE … RETURNING does selection and lease assignment in one
// round-trip; FOR UPDATE SKIP LOCKED lets consumers split the work. The lease
// cutoff is computed in Go (now - leaseTimeout) and the lease id is generated
// Go-side and shared by the whole batch (one per Claim).
func (q *PG) Claim(ctx context.Context, now time.Time, limit int) ([]Task, error) {
	const query = `
WITH next AS (
    SELECT id FROM queue_tasks
    WHERE done_at IS NULL
      AND run_at <= :now
      AND (lease_id IS NULL OR leased_at < :lease_cutoff)
    ORDER BY run_at, id
    LIMIT :limit
    FOR UPDATE SKIP LOCKED
)
UPDATE queue_tasks t
SET lease_id = :lease_id, leased_at = :now, attempts = attempts + 1
FROM next
WHERE t.id = next.id
RETURNING t.id, t.name, t.kind, t.payload, t.created_at, t.run_at,
          t.attempts, t.lease_id, t.last_error`
	args := struct {
		Now         time.Time `db:"now"`
		LeaseCutoff time.Time `db:"lease_cutoff"`
		LeaseID     string    `db:"lease_id"`
		Limit       int       `db:"limit"`
	}{Now: now, LeaseCutoff: now.Add(-q.leaseTimeout), LeaseID: uuid.NewString(), Limit: limit}

	var rows []taskRow
	if err := dbx.NamedQuerySlice(ctx, q.log, q.db, query, args, &rows); err != nil {
		return nil, fmt.Errorf("queue claim: %w", err)
	}
	return toTasks(rows), nil
}

// MarkDone removes a successfully processed task. The lease_id predicate is the
// lease guard: a consumer that reclaimed the task after the lease expired holds
// a different lease_id, so the DELETE matches no row and we return ErrLeaseLost.
func (q *PG) MarkDone(ctx context.Context, t Task, _ time.Time) error {
	const query = `DELETE FROM queue_tasks WHERE id = :id AND lease_id = :lease_id`
	args := struct {
		ID      int64  `db:"id"`
		LeaseID string `db:"lease_id"`
	}{ID: t.ID, LeaseID: t.LeaseID}

	n, err := dbx.NamedExecContextRowsAffected(ctx, q.log, q.db, query, args)
	if err != nil {
		return fmt.Errorf("queue mark done: %w", err)
	}
	if n == 0 {
		return ErrLeaseLost
	}
	return nil
}

// MarkFailed releases a failed task for retry (terminal=false: clears the lease
// and reschedules run_at to now+retryDelay so a later Claim retries it without
// busy-looping) or parks it as a dead letter (terminal=true: sets done_at so
// Claim skips it but Cleanup can reap it). Guarded by the same lease predicate
// as MarkDone.
func (q *PG) MarkFailed(ctx context.Context, t Task, errMsg string, terminal bool, now time.Time) error {
	const retryQuery = `
UPDATE queue_tasks SET lease_id = NULL, leased_at = NULL, last_error = :last_error, run_at = :next_run
WHERE id = :id AND lease_id = :lease_id AND done_at IS NULL`
	const deadQuery = `
UPDATE queue_tasks SET done_at = :now, lease_id = NULL, last_error = :last_error
WHERE id = :id AND lease_id = :lease_id AND done_at IS NULL`
	args := struct {
		ID        int64     `db:"id"`
		LeaseID   string    `db:"lease_id"`
		LastError string    `db:"last_error"`
		Now       time.Time `db:"now"`
		NextRun   time.Time `db:"next_run"`
	}{ID: t.ID, LeaseID: t.LeaseID, LastError: errMsg, Now: now, NextRun: now.Add(q.retryDelay)}

	query := retryQuery
	if terminal {
		query = deadQuery
	}
	n, err := dbx.NamedExecContextRowsAffected(ctx, q.log, q.db, query, args)
	if err != nil {
		return fmt.Errorf("queue mark failed: %w", err)
	}
	if n == 0 {
		return ErrLeaseLost
	}
	return nil
}

// Cleanup removes dead-lettered/done tasks finished before the given time,
// returning how many rows were deleted. Run it periodically via worker.Loop.
func (q *PG) Cleanup(ctx context.Context, before time.Time) (int64, error) {
	const query = `DELETE FROM queue_tasks WHERE done_at IS NOT NULL AND done_at < :before`
	args := struct {
		Before time.Time `db:"before"`
	}{Before: before}

	n, err := dbx.NamedExecContextRowsAffected(ctx, q.log, q.db, query, args)
	if err != nil {
		return 0, fmt.Errorf("queue cleanup: %w", err)
	}
	return n, nil
}
