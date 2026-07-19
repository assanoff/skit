package lock

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/assanoff/skit/dbx"
)

// PG is a Locker backed by Postgres session-scoped advisory locks
// (pg_try_advisory_lock). Each TryLock checks out a dedicated connection and
// holds the lock on it; Release unlocks and returns the connection. If the
// holder crashes, Postgres drops the connection and releases the lock
// automatically, so a lock can never leak — this is why PG ignores ttl.
type PG struct {
	db  *sqlx.DB
	log *slog.Logger
}

// NewPG builds a Postgres advisory-lock Locker over db. log may be nil.
func NewPG(db *sqlx.DB, log *slog.Logger) *PG {
	if log == nil {
		log = slog.Default()
	}
	return &PG{db: db, log: log}
}

var _ Locker = (*PG)(nil)

// TryLock acquires pg_try_advisory_lock(key-hash) on a dedicated connection. ttl
// is ignored (the lock is session-scoped and released on Release or connection
// death). ok==false means another session holds the lock.
func (p *PG) TryLock(ctx context.Context, key string, _ time.Duration) (Release, bool, error) {
	lockKey := dbx.AdvisoryKey(key)

	conn, err := p.db.Connx(ctx)
	if err != nil {
		return noopRelease, false, fmt.Errorf("lock: acquire connection: %w", err)
	}

	var got bool
	if err := conn.QueryRowxContext(ctx, "SELECT pg_try_advisory_lock($1)", lockKey).Scan(&got); err != nil {
		_ = conn.Close()
		return noopRelease, false, fmt.Errorf("lock: pg_try_advisory_lock: %w", err)
	}
	if !got {
		_ = conn.Close() // release the connection; someone else holds the lock
		return noopRelease, false, nil
	}

	// Release must run on the SAME connection that took the lock. Use a fresh
	// context so unlock still runs if the caller's ctx is already canceled.
	release := func() {
		defer func() { _ = conn.Close() }()
		uctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(uctx, "SELECT pg_advisory_unlock($1)", lockKey); err != nil {
			p.log.Warn("lock: pg_advisory_unlock failed", "key", key, "error", err)
		}
	}
	return release, true, nil
}
