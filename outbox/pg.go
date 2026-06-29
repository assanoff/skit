package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/logger"
	"github.com/assanoff/skit/retry"
)

// PG is the Postgres-backed implementation of Store (and StatsReader). Every
// query is an inline const right next to the method that runs it — no
// string-templated query cache, no fmt.Sprintf — so the SQL reads as SQL. The
// backing table is fixed (outbox_events); status values are bound as named
// params from the Status* constants so the SQL and Go agree on one source of
// truth.
//
// All statements go through the dbx named-query helpers for consistent
// tracing and error wrapping. The Store holds its handle as sqlx.ExtContext, so
// the same type works against either a *sqlx.DB (pool-bound) or a *sqlx.Tx
// (transaction-bound, via WithTx) — that is how Insert joins the caller's
// domain transaction in WithinTran.
//
// PG is safe for concurrent use by multiple relay/sweeper replicas:
// LeasePending and SweepExpiredLeases claim rows with FOR UPDATE SKIP LOCKED.
type PG struct {
	log     *logger.Logger
	db      sqlx.ExtContext
	backoff retry.Backoff
}

var (
	_ Store       = (*PG)(nil)
	_ StatsReader = (*PG)(nil)
)

// Options configures a PG store.
type Options struct {
	// Backoff schedules retries (next_attempt_at) for failed-but-retryable
	// events. Defaults to base 2s, factor 2, max 5m.
	Backoff retry.Backoff
}

// NewPG builds a Postgres outbox store. The table is created by a migration
// (or EnsureSchema in tests), not here.
func NewPG(log *logger.Logger, db *sqlx.DB, opts Options) *PG {
	bo := opts.Backoff
	if bo.Base == 0 {
		bo.Base = 2 * time.Second
	}
	if bo.Factor == 0 {
		bo.Factor = 2
	}
	if bo.Max == 0 {
		bo.Max = 5 * time.Minute
	}
	return &PG{log: log, db: db, backoff: bo}
}

// WithTx returns a sibling store whose queries run on tx.
func (s *PG) WithTx(tx sqlx.ExtContext) Store {
	c := *s
	c.db = tx
	return &c
}

// EnsureSchema creates the outbox table and indexes if absent.
func (s *PG) EnsureSchema(ctx context.Context) error {
	return dbx.ExecContext(ctx, s.log, s.db, Schema())
}

// Insert writes pending events. Bound to the caller's transaction when the
// store was derived via WithTx.
func (s *PG) Insert(ctx context.Context, events ...Event) error {
	const q = `
INSERT INTO outbox_events (
    id, type, content_type, topic, route_key, payload, headers,
    status, attempts, max_attempts, last_error, next_attempt_at, created_at
) VALUES (
    :id, :type, :content_type, :topic, :route_key, :payload, :headers,
    :status, :attempts, :max_attempts, :last_error, :next_attempt_at, :created_at
)`
	for i := range events {
		// Headers is JSONB: NewEvent guarantees valid JSON, but Insert is the
		// boundary to the DB and we don't trust callers that bypass NewEvent.
		if len(events[i].Headers) == 0 || !json.Valid(events[i].Headers) {
			return fmt.Errorf("outbox insert: invalid headers JSON for event %s", events[i].ID)
		}
		if err := dbx.NamedExecContext(ctx, s.log, s.db, q, toDBRow(events[i])); err != nil {
			return fmt.Errorf("outbox insert: %w", err)
		}
	}
	return nil
}

// LeasePending atomically claims up to limit pending events whose
// next_attempt_at <= now and marks them in_flight under a fresh lease id. A
// single CTE + UPDATE … RETURNING does selection and lease assignment in one
// round-trip; FOR UPDATE SKIP LOCKED lets relay replicas split the work. The
// whole batch shares one lease id (one per relay tick).
func (s *PG) LeasePending(ctx context.Context, now time.Time, limit int) ([]Event, error) {
	const q = `
WITH cte AS (
    SELECT id FROM outbox_events
    WHERE status = :pending AND next_attempt_at <= :now
    ORDER BY next_attempt_at, created_at
    LIMIT :limit
    FOR UPDATE SKIP LOCKED
)
UPDATE outbox_events e
SET status = :in_flight, leased_at = :now, lease_id = :lease_id
FROM cte
WHERE e.id = cte.id
RETURNING e.id, e.type, e.content_type, e.topic, e.route_key, e.payload, e.headers,
          e.status, e.attempts, e.max_attempts, e.last_error,
          e.next_attempt_at, e.created_at, e.sent_at, e.leased_at, e.lease_id`
	args := struct {
		Pending  string    `db:"pending"`
		InFlight string    `db:"in_flight"`
		Now      time.Time `db:"now"`
		Limit    int       `db:"limit"`
		LeaseID  uuid.UUID `db:"lease_id"`
	}{
		Pending:  StatusPending,
		InFlight: StatusInFlight,
		Now:      now,
		Limit:    limit,
		LeaseID:  uuid.New(),
	}

	var rows []rowDB
	if err := dbx.NamedQuerySlice(ctx, s.log, s.db, q, args, &rows); err != nil {
		return nil, fmt.Errorf("outbox lease pending: %w", err)
	}
	return toCoreRows(rows), nil
}

// MarkSent transitions in_flight -> sent in a single guarded UPDATE. The
// lease_id + status='in_flight' predicate is the lease guard: a sweeper or
// parallel relay that reclaimed the row clears its lease_id, so the UPDATE
// matches no row and we return ErrLeaseLost.
func (s *PG) MarkSent(ctx context.Context, ev Event, leaseID uuid.UUID, now time.Time) error {
	const q = `
UPDATE outbox_events
SET status = :sent, sent_at = :now, leased_at = NULL, lease_id = NULL
WHERE id = :id AND lease_id = :lease_id AND status = :in_flight`
	args := struct {
		Sent     string    `db:"sent"`
		InFlight string    `db:"in_flight"`
		Now      time.Time `db:"now"`
		ID       uuid.UUID `db:"id"`
		LeaseID  uuid.UUID `db:"lease_id"`
	}{Sent: StatusSent, InFlight: StatusInFlight, Now: now, ID: ev.ID, LeaseID: leaseID}

	n, err := dbx.NamedExecContextRowsAffected(ctx, s.log, s.db, q, args)
	if err != nil {
		return fmt.Errorf("outbox mark sent: %w", err)
	}
	if n == 0 {
		return ErrLeaseLost
	}
	return nil
}

// MarkFailed transitions in_flight -> pending (with backoff) when the retry
// budget remains, or -> failed once attempts+1 reaches max_attempts. Guarded by
// the same lease predicate as MarkSent.
func (s *PG) MarkFailed(ctx context.Context, ev Event, leaseID uuid.UUID, errMsg string, now time.Time) error {
	attempts := ev.Attempts + 1
	status := StatusPending
	nextAttempt := now.Add(s.backoff.Next(attempts))
	if attempts >= ev.MaxAttempts {
		status = StatusFailed
		nextAttempt = now // terminal; value is informational
	}

	const q = `
UPDATE outbox_events
SET status = :status, attempts = :attempts, last_error = :last_error,
    next_attempt_at = :next_attempt_at, leased_at = NULL, lease_id = NULL
WHERE id = :id AND lease_id = :lease_id AND status = :in_flight`
	args := struct {
		Status        string    `db:"status"`
		InFlight      string    `db:"in_flight"`
		Attempts      int       `db:"attempts"`
		LastError     string    `db:"last_error"`
		NextAttemptAt time.Time `db:"next_attempt_at"`
		ID            uuid.UUID `db:"id"`
		LeaseID       uuid.UUID `db:"lease_id"`
	}{
		Status:        status,
		InFlight:      StatusInFlight,
		Attempts:      attempts,
		LastError:     errMsg,
		NextAttemptAt: nextAttempt,
		ID:            ev.ID,
		LeaseID:       leaseID,
	}

	n, err := dbx.NamedExecContextRowsAffected(ctx, s.log, s.db, q, args)
	if err != nil {
		return fmt.Errorf("outbox mark failed: %w", err)
	}
	if n == 0 {
		return ErrLeaseLost
	}
	return nil
}

// SweepExpiredLeases returns in_flight rows whose lease is older than
// leaseTimeout back to pending so a crashed relay's work is retried. attempts
// is left unchanged — a lease expiry means the worker died, not that the broker
// rejected the message. Returns the number reclaimed.
func (s *PG) SweepExpiredLeases(ctx context.Context, leaseTimeout time.Duration, now time.Time, limit int) (int64, error) {
	const q = `
WITH expired AS (
    SELECT id FROM outbox_events
    WHERE status = :in_flight AND leased_at < :cutoff
    ORDER BY leased_at
    LIMIT :limit
    FOR UPDATE SKIP LOCKED
)
UPDATE outbox_events e
SET status = :pending, leased_at = NULL, lease_id = NULL, next_attempt_at = :now
FROM expired
WHERE e.id = expired.id`
	args := struct {
		InFlight string    `db:"in_flight"`
		Pending  string    `db:"pending"`
		Cutoff   time.Time `db:"cutoff"`
		Now      time.Time `db:"now"`
		Limit    int       `db:"limit"`
	}{
		InFlight: StatusInFlight,
		Pending:  StatusPending,
		Cutoff:   now.Add(-leaseTimeout),
		Now:      now,
		Limit:    limit,
	}

	n, err := dbx.NamedExecContextRowsAffected(ctx, s.log, s.db, q, args)
	if err != nil {
		return 0, fmt.Errorf("outbox sweep: %w", err)
	}
	return n, nil
}

// Cleanup deletes terminal (sent/failed) rows created before now-retention.
// Returns the number of rows deleted.
func (s *PG) Cleanup(ctx context.Context, retention time.Duration, now time.Time) (int64, error) {
	const q = `
DELETE FROM outbox_events
WHERE status IN (:sent, :failed) AND created_at < :cutoff`
	args := struct {
		Sent   string    `db:"sent"`
		Failed string    `db:"failed"`
		Cutoff time.Time `db:"cutoff"`
	}{Sent: StatusSent, Failed: StatusFailed, Cutoff: now.Add(-retention)}

	n, err := dbx.NamedExecContextRowsAffected(ctx, s.log, s.db, q, args)
	if err != nil {
		return 0, fmt.Errorf("outbox cleanup: %w", err)
	}
	return n, nil
}

// Stats returns a backlog snapshot (pending/in_flight/failed counts and the age
// of the oldest pending row) in one aggregate query. Used by BacklogCollector
// to expose backlog gauges.
func (s *PG) Stats(ctx context.Context, now time.Time) (Stats, error) {
	const q = `
SELECT
    count(*) FILTER (WHERE status = :pending)   AS pending,
    count(*) FILTER (WHERE status = :in_flight) AS in_flight,
    count(*) FILTER (WHERE status = :failed)    AS failed,
    COALESCE(EXTRACT(EPOCH FROM (:now - min(created_at) FILTER (WHERE status = :pending))), 0) AS oldest_pending_secs
FROM outbox_events`
	args := struct {
		Pending  string    `db:"pending"`
		InFlight string    `db:"in_flight"`
		Failed   string    `db:"failed"`
		Now      time.Time `db:"now"`
	}{Pending: StatusPending, InFlight: StatusInFlight, Failed: StatusFailed, Now: now}

	var row struct {
		Pending           int64   `db:"pending"`
		InFlight          int64   `db:"in_flight"`
		Failed            int64   `db:"failed"`
		OldestPendingSecs float64 `db:"oldest_pending_secs"`
	}
	if err := dbx.NamedQueryStruct(ctx, s.log, s.db, q, args, &row); err != nil {
		return Stats{}, fmt.Errorf("outbox stats: %w", err)
	}
	return Stats{
		Pending:          row.Pending,
		InFlight:         row.InFlight,
		Failed:           row.Failed,
		OldestPendingAge: time.Duration(row.OldestPendingSecs * float64(time.Second)),
	}, nil
}

// Schema returns the DDL creating the outbox_events table and its pending-scan
// index. Apply it in a migration, or call PG.EnsureSchema in tests.
func Schema() string {
	return `
CREATE TABLE IF NOT EXISTS outbox_events (
    id              UUID PRIMARY KEY,
    type            TEXT NOT NULL,
    content_type    TEXT NOT NULL,
    topic           TEXT NOT NULL,
    route_key       TEXT NOT NULL DEFAULT '',
    payload         BYTEA NOT NULL,
    headers         JSONB NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'pending',
    attempts        INT NOT NULL DEFAULT 0,
    max_attempts    INT NOT NULL DEFAULT 10,
    last_error      TEXT,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at         TIMESTAMPTZ,
    leased_at       TIMESTAMPTZ,
    lease_id        UUID
);
CREATE INDEX IF NOT EXISTS outbox_events_pending_idx ON outbox_events (status, next_attempt_at);`
}
