package lock_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/matryer/is"
	"github.com/redis/go-redis/v9"

	"github.com/assanoff/skit/dbtest"
	"github.com/assanoff/skit/lock"
)

// lockerContract exercises the behavior every Locker must share: one holder at a
// time per key, release frees it, and distinct keys are independent.
func lockerContract(t *testing.T, lk lock.Locker) {
	t.Helper()
	is := is.New(t)
	ctx := context.Background()
	ttl := time.Minute

	rel1, ok1, err := lk.TryLock(ctx, "job-a", ttl)
	is.NoErr(err)
	is.True(ok1) // first acquire succeeds

	_, ok2, err := lk.TryLock(ctx, "job-a", ttl)
	is.NoErr(err)
	is.True(!ok2) // same key is held -> denied

	relB, okB, err := lk.TryLock(ctx, "job-b", ttl)
	is.NoErr(err)
	is.True(okB) // a different key is independent
	relB()

	rel1() // release job-a

	rel3, ok3, err := lk.TryLock(ctx, "job-a", ttl)
	is.NoErr(err)
	is.True(ok3) // re-acquire after release
	rel3()
}

func TestPGLocker(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires docker")
	}
	ctx := context.Background()
	pg := dbtest.NewPostgres(ctx, t, dbtest.Config{})
	lockerContract(t, lock.NewPG(pg.DB, nil))
}

func TestRedisLocker(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	lockerContract(t, lock.NewRedis(client, nil))
}

func TestRedisLockerRequiresTTL(t *testing.T) {
	is := is.New(t)
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	_, ok, err := lock.NewRedis(client, nil).TryLock(context.Background(), "k", 0)
	is.True(!ok)        // not acquired
	is.True(err != nil) // ttl <= 0 is rejected
}

func TestRedisLockExpiresAndReleaseIsFenced(t *testing.T) {
	is := is.New(t)
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	lk := lock.NewRedis(client, nil)
	ctx := context.Background()

	rel1, ok1, err := lk.TryLock(ctx, "job", 50*time.Millisecond)
	is.NoErr(err)
	is.True(ok1)

	mr.FastForward(60 * time.Millisecond) // lease expires

	// A second holder now grabs it (previous lease gone).
	_, ok2, err := lk.TryLock(ctx, "job", time.Minute)
	is.NoErr(err)
	is.True(ok2)

	// The first holder's Release must NOT free the second holder's lock — the
	// token fence protects it.
	rel1()
	_, ok3, err := lk.TryLock(ctx, "job", time.Minute)
	is.NoErr(err)
	is.True(!ok3) // still held by the second holder
}
