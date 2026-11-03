// Package lock provides distributed mutual exclusion for work that must run on
// at most one replica at a time — most often a scheduled job (see the cron
// package and `skit add cron --lock`). A Locker hands out a short-lived,
// best-effort lock keyed by a name; the holder does the work and releases it.
//
// # Backends
//
//   - PG: Postgres advisory locks, pg_try_advisory_lock on a dedicated
//     connection. No infrastructure beyond the database the service already
//     has; the lock goes when the holding connection dies, so PG ignores ttl.
//   - Redis: SET NX PX and a release that checks a random token. The ttl
//     bounds how long a crashed holder blocks the others; the token keeps one
//     holder from freeing the lock of another.
//
// # Usage
//
// TryLock never blocks: it takes the lock now or reports it is held
// elsewhere. A caller that misses the lock skips this round rather than
// waits — the right behavior for periodic jobs:
//
//	locker := lock.NewPG(db, log) // or lock.NewRedis(client, log)
//
//	release, ok, err := locker.TryLock(ctx, "report", time.Minute)
//	if err != nil {
//	    return err // the store is unreachable: the lock may be free or not
//	}
//	if !ok {
//	    return nil // another replica runs the job
//	}
//	defer release()
//	return runReport(ctx)
//
// A periodic job that ends quickly keeps the lock a while longer, until the
// ticks of the other replicas are past: released at once, it lets a replica
// whose tick fires a moment later run the job again. The template of
// `skit add cron --lock` does so.
package lock
