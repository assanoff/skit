// Package lock provides distributed mutual exclusion for work that must run on
// at most one replica at a time — most commonly a scheduled job (see the cron
// package and `skit add cron`). A Locker hands out a short-lived, best-effort
// lock keyed by a name; the holder does the work and releases it.
//
// Two backends implement Locker:
//
//   - PG (Postgres advisory locks): pg_try_advisory_lock on a dedicated
//     connection. No external infrastructure beyond the database you already
//     have; the lock releases automatically if the holding connection dies.
//   - Redis (SET NX PX + a token-checked release): a TTL bounds how long a
//     crashed holder can block others; release is fenced by a random token so
//     one holder never frees another's lock.
//
// The contract is deliberately minimal and non-blocking: TryLock either grabs
// the lock now or reports it is held elsewhere. Callers that miss the lock skip
// this round rather than queue — the right behavior for periodic jobs.
package lock

import (
	"context"
	"time"
)

// Release frees a held lock. It is safe to call exactly once; implementations
// make a best effort and never panic. A Release returned with ok==false from
// TryLock is a no-op.
type Release func()

// Locker grants best-effort, single-holder locks.
type Locker interface {
	// TryLock attempts to acquire the lock named key without blocking. On
	// success it returns ok==true and a Release to free the lock. If another
	// holder has it, ok==false and Release is a no-op. err is non-nil only on an
	// infrastructure failure (the caller cannot tell whether the lock is free).
	//
	// ttl bounds how long the lock survives a crashed holder: the Redis backend
	// expires the key after ttl, while the Postgres backend ignores ttl and
	// relies on connection death (its lock is session-scoped). Hold the lock only
	// for the duration of the guarded work, then Release.
	TryLock(ctx context.Context, key string, ttl time.Duration) (Release, bool, error)
}

// noopRelease is returned whenever a lock was not acquired.
func noopRelease() {}
