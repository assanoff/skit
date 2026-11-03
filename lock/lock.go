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
	// relies on connection death (its lock is session-scoped). Hold the lock
	// while the guarded work needs it, then Release; with Redis keep ttl above
	// that time.
	TryLock(ctx context.Context, key string, ttl time.Duration) (Release, bool, error)
}

// noopRelease is returned whenever a lock was not acquired.
func noopRelease() {}
