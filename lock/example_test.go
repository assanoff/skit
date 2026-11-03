package lock_test

import (
	"context"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/assanoff/skit/cron"
	"github.com/assanoff/skit/dbx/pg"
	"github.com/assanoff/skit/lock"
)

// A cron job fires on every replica; the lock lets one of them run it. A
// replica that misses the lock skips this round. The example needs Postgres,
// so go test builds it but does not run it.
func Example() {
	ctx := context.Background()
	log := slog.Default()
	db, err := pg.Open(pg.Config{Host: "localhost:5432", Name: "shop", DisableTLS: true})
	if err != nil {
		log.ErrorContext(ctx, "open", "err", err)
		return
	}
	defer func() { _ = db.Close() }()
	locker := lock.NewPG(db, log)

	sched := cron.New(log, cron.Config{Name: "reports"})
	err = sched.Add("0 3 * * *", "nightly-report", func(ctx context.Context) error {
		release, ok, err := locker.TryLock(ctx, "nightly-report", time.Minute)
		if err != nil {
			return err // the store is unreachable: the lock may be free or not
		}
		if !ok {
			return nil // another replica runs the job
		}
		defer release()
		log.InfoContext(ctx, "building the nightly report")
		return nil
	})
	if err != nil {
		log.ErrorContext(ctx, "cron", "err", err)
	}
}

// Redis takes the lock with SET NX PX: the ttl frees it when the holder
// crashes, so keep it above the time the work takes.
func ExampleNewRedis() {
	ctx := context.Background()
	log := slog.Default()
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer func() { _ = client.Close() }()
	locker := lock.NewRedis(client, log)

	release, ok, err := locker.TryLock(ctx, "reindex", 10*time.Minute)
	if err != nil || !ok {
		return
	}
	defer release()
	log.InfoContext(ctx, "reindexing")
}
