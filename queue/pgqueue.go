package queue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	"github.com/assanoff/skit/dbx"
)

// errLeaseLost is returned by a mark when the lease of the task no longer
// matches: another consumer reclaimed it after the lease expired. The mark is
// a no-op; the other consumer now owns the task.
var errLeaseLost = errors.New("queue: lease lost")

// PG is the Postgres store of every queue: one table, queue_tasks, where the
// queue column tells the queues apart. Schedule enqueues; the workers of a
// queue (Workers) claim, mark and release its tasks through the unexported
// methods. Every query is an inline const right next to the method that runs
// it, and all time math and lease ids happen in Go, so the SQL stays free of
// NOW()/gen_random_uuid().
//
// PG is safe for concurrent use by goroutines and replicas: a claim takes
// rows with FOR UPDATE SKIP LOCKED, so each ready task goes to one consumer
// at a time.
type PG struct {
	log *slog.Logger
	db  *dbx.DB
}

// NewPG builds the store over db. It does not create the table; call
// EnsureSchema at startup.
func NewPG(log *slog.Logger, db *dbx.DB) *PG {
	return &PG{log: log, db: db}
}

// schemaLockKey keys the transaction-level advisory lock that serializes
// EnsureSchema across replicas booting at once.
var schemaLockKey = dbx.AdvisoryKey("skit.queue.schema")

// EnsureSchema creates the table and its indexes if they do not exist. It is
// safe to call at startup, including from several replicas at once: the DDL
// runs under a transaction-scoped advisory lock, so concurrent boots serialize
// instead of racing on CREATE TABLE.
func (q *PG) EnsureSchema(ctx context.Context) error {
	return dbx.EnsureSchema(ctx, q.log, q.db, schemaLockKey, schema())
}

// Schedule enqueues a task on the pool and reports whether it was inserted:
// false when a task with the same Key is already in the queue. A producer
// schedules in its transaction through Bind.
func (q *PG) Schedule(ctx context.Context, p Params) (bool, error) {
	return q.insert(ctx, q.db, p)
}

// insert writes the task of p on db — the pool for Schedule, the transaction
// of a producer for a Scheduler — and reports whether it was inserted.
func (q *PG) insert(ctx context.Context, db dbx.Querier, p Params) (bool, error) {
	switch {
	case p.Queue == "":
		return false, errors.New("queue schedule: queue is required")
	case p.Kind == "":
		return false, errors.New("queue schedule: kind is required")
	}
	const query = `
INSERT INTO queue_tasks (queue, kind, key, payload, run_at)
VALUES (:queue, :kind, :key, :payload, :run_at)
ON CONFLICT (queue, key) WHERE key IS NOT NULL DO NOTHING`
	args := struct {
		Queue   string         `db:"queue"`
		Kind    string         `db:"kind"`
		Key     sql.NullString `db:"key"`
		Payload []byte         `db:"payload"`
		RunAt   time.Time      `db:"run_at"`
	}{
		Queue:   p.Queue,
		Kind:    p.Kind,
		Key:     sql.NullString{String: p.Key, Valid: p.Key != ""},
		Payload: p.Payload,
		RunAt:   time.Now().UTC().Add(p.Delay),
	}

	n, err := dbx.NamedExecRowsAffected(ctx, db, query, args)
	if err != nil {
		return false, fmt.Errorf("queue schedule: %w", err)
	}
	return n == 1, nil
}

// claim leases up to limit ready tasks of queue — run_at reached, not leased
// or leased before now - lease — under a fresh lease id shared by the batch,
// and returns them in run_at, id order. A single statement does selection and
// lease assignment in one round-trip; FOR UPDATE SKIP LOCKED lets consumers
// split the work, and the outer SELECT orders the batch, which UPDATE …
// RETURNING alone returns in storage order.
func (q *PG) claim(ctx context.Context, queue string, now time.Time, lease time.Duration, limit int) ([]leased, error) {
	const query = `
WITH next AS (
    SELECT id FROM queue_tasks
    WHERE queue = :queue
      AND done_at IS NULL
      AND run_at <= :now
      AND (lease_id IS NULL OR leased_at < :lease_cutoff)
    ORDER BY run_at, id
    LIMIT :limit
    FOR UPDATE SKIP LOCKED
), leased AS (
    UPDATE queue_tasks t
    SET lease_id = :lease_id, leased_at = :now
    FROM next
    WHERE t.id = next.id
    RETURNING t.*
)
SELECT id, queue, kind, key, payload, created_at, run_at, attempts, lease_id, last_error
FROM leased
ORDER BY run_at, id`
	args := struct {
		Queue       string    `db:"queue"`
		Now         time.Time `db:"now"`
		LeaseCutoff time.Time `db:"lease_cutoff"`
		LeaseID     string    `db:"lease_id"`
		Limit       int       `db:"limit"`
	}{Queue: queue, Now: now, LeaseCutoff: now.Add(-lease), LeaseID: uuid.New().String(), Limit: limit}

	rows, err := dbx.NamedQueryList[taskRow](ctx, q.db, query, args)
	if err != nil {
		return nil, fmt.Errorf("queue claim: %w", err)
	}
	return toLeasedAll(rows), nil
}

// markDone removes a handled task. The lease_id predicate is the lease guard:
// a consumer that reclaimed the task after the lease expired holds another
// lease id, so the DELETE matches no row and markDone returns errLeaseLost.
func (q *PG) markDone(ctx context.Context, t leased) error {
	const query = `DELETE FROM queue_tasks WHERE id = :id AND lease_id = :lease_id`
	args := struct {
		ID      int64  `db:"id"`
		LeaseID string `db:"lease_id"`
	}{ID: t.ID, LeaseID: t.leaseID}

	n, err := dbx.NamedExecRowsAffected(ctx, q.db, query, args)
	if err != nil {
		return fmt.Errorf("queue mark done: %w", err)
	}
	if n == 0 {
		return errLeaseLost
	}
	return nil
}

// markFailed counts a failed attempt of t and records errMsg. A dead task
// stays in the table with done_at set, so no claim takes it again and the
// cleaner removes it later; any other is released to run again at runAt.
// Guarded by the lease, as markDone.
func (q *PG) markFailed(ctx context.Context, t leased, errMsg string, dead bool, now, runAt time.Time) error {
	const retryQuery = `
UPDATE queue_tasks
SET lease_id = NULL, leased_at = NULL, attempts = :attempts, last_error = :last_error,
    run_at = :run_at
WHERE id = :id AND lease_id = :lease_id`
	const deadQuery = `
UPDATE queue_tasks
SET lease_id = NULL, leased_at = NULL, attempts = :attempts, last_error = :last_error,
    done_at = :now
WHERE id = :id AND lease_id = :lease_id`
	args := struct {
		ID        int64     `db:"id"`
		LeaseID   string    `db:"lease_id"`
		Attempts  int       `db:"attempts"`
		LastError string    `db:"last_error"`
		Now       time.Time `db:"now"`
		RunAt     time.Time `db:"run_at"`
	}{ID: t.ID, LeaseID: t.leaseID, Attempts: t.Attempts + 1, LastError: errMsg, Now: now, RunAt: runAt}

	query := retryQuery
	if dead {
		query = deadQuery
	}
	n, err := dbx.NamedExecRowsAffected(ctx, q.db, query, args)
	if err != nil {
		return fmt.Errorf("queue mark failed: %w", err)
	}
	if n == 0 {
		return errLeaseLost
	}
	return nil
}

// release returns tasks of one claim to the queue untouched: their lease is
// cleared, and they keep their place and their attempts. A task no longer
// under the lease of the first task stays as it is.
func (q *PG) release(ctx context.Context, tasks []leased) error {
	if len(tasks) == 0 {
		return nil
	}
	const query = `
UPDATE queue_tasks SET lease_id = NULL, leased_at = NULL
WHERE id = ANY(:ids) AND lease_id = :lease_id`
	ids := make([]int64, len(tasks))
	for i := range tasks {
		ids[i] = tasks[i].ID
	}
	args := struct {
		IDs     []int64 `db:"ids"`
		LeaseID string  `db:"lease_id"`
	}{IDs: ids, LeaseID: tasks[0].leaseID}

	if err := dbx.NamedExec(ctx, q.db, query, args); err != nil {
		return fmt.Errorf("queue release: %w", err)
	}
	return nil
}

// cleanup removes the dead tasks of queue that died earlier than before and
// returns how many it removed.
func (q *PG) cleanup(ctx context.Context, queue string, before time.Time) (int64, error) {
	const query = `
DELETE FROM queue_tasks
WHERE queue = :queue AND done_at IS NOT NULL AND done_at < :before`
	args := struct {
		Queue  string    `db:"queue"`
		Before time.Time `db:"before"`
	}{Queue: queue, Before: before}

	n, err := dbx.NamedExecRowsAffected(ctx, q.db, query, args)
	if err != nil {
		return 0, fmt.Errorf("queue cleanup: %w", err)
	}
	return n, nil
}

// stats is the backlog of one queue at a point in time.
type stats struct {
	Pending int64 `db:"pending"` // ready to claim: not leased, run_at reached
	Leased  int64 `db:"leased"`  // claimed by a consumer, including expired leases
	Dead    int64 `db:"dead"`    // dead letters awaiting the cleaner
}

// stats reads the backlog of queue at now. Each count is its own subquery, so
// it reads the partial index of its rows instead of the whole table.
func (q *PG) stats(ctx context.Context, queue string, now time.Time) (stats, error) {
	const query = `
SELECT
    (SELECT count(*) FROM queue_tasks
     WHERE queue = :queue AND done_at IS NULL AND run_at <= :now AND lease_id IS NULL) AS pending,
    (SELECT count(*) FROM queue_tasks
     WHERE queue = :queue AND done_at IS NULL AND lease_id IS NOT NULL) AS leased,
    (SELECT count(*) FROM queue_tasks
     WHERE queue = :queue AND done_at IS NOT NULL) AS dead`
	args := struct {
		Queue string    `db:"queue"`
		Now   time.Time `db:"now"`
	}{Queue: queue, Now: now}

	st, err := dbx.NamedQueryOne[stats](ctx, q.db, query, args)
	if err != nil {
		return stats{}, fmt.Errorf("queue stats: %w", err)
	}
	return st, nil
}

// schema returns the DDL of queue_tasks: the key index deduplicates within a
// queue, the ready index serves the claim and the dead index the cleaner and
// the dead count.
func schema() string {
	return `
CREATE TABLE IF NOT EXISTS queue_tasks (
    id         BIGSERIAL PRIMARY KEY,
    queue      TEXT NOT NULL,
    kind       TEXT NOT NULL,
    key        TEXT,
    payload    BYTEA,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    run_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    attempts   INT NOT NULL DEFAULT 0,
    leased_at  TIMESTAMPTZ,
    lease_id   TEXT,
    last_error TEXT,
    done_at    TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS queue_tasks_key_idx
    ON queue_tasks (queue, key) WHERE key IS NOT NULL;
CREATE INDEX IF NOT EXISTS queue_tasks_ready_idx
    ON queue_tasks (queue, run_at, id) WHERE done_at IS NULL;
CREATE INDEX IF NOT EXISTS queue_tasks_dead_idx
    ON queue_tasks (queue, done_at) WHERE done_at IS NOT NULL;`
}
