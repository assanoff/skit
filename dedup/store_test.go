package dedup

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/matryer/is"
	"github.com/redis/go-redis/v9"
)

// storeContract exercises the behavior every store must share. advance moves
// the clock of the store forward.
func storeContract(t *testing.T, st store, advance func(time.Duration)) {
	t.Helper()
	is := is.New(t)
	ctx := context.Background()
	lease, ttl := time.Minute, time.Hour

	got, _, err := st.acquire(ctx, "k1", "t1", lease)
	is.NoErr(err)
	is.Equal(got, acquired) // a new key is acquired

	got, _, err = st.acquire(ctx, "k1", "t2", lease)
	is.NoErr(err)
	is.Equal(got, running) // a held key is running

	ok, err := st.complete(ctx, "k1", "t2", []byte(`"x"`), ttl)
	is.NoErr(err)
	is.True(!ok) // another token does not complete
	is.NoErr(st.release(ctx, "k1", "t2"))
	got, _, err = st.acquire(ctx, "k1", "t3", lease)
	is.NoErr(err)
	is.Equal(got, running) // nor release

	ok, err = st.complete(ctx, "k1", "t1", []byte(`"v1"`), ttl)
	is.NoErr(err)
	is.True(ok) // the holder completes
	got, value, err := st.acquire(ctx, "k1", "t4", lease)
	is.NoErr(err)
	is.Equal(got, done) // a completed key is done
	is.Equal(string(value), `"v1"`)
	is.NoErr(st.release(ctx, "k1", "t1"))
	got, _, err = st.acquire(ctx, "k1", "t5", lease)
	is.NoErr(err)
	is.Equal(got, done) // release leaves a done key alone

	_, _, err = st.acquire(ctx, "k2", "t1", lease)
	is.NoErr(err)
	is.NoErr(st.release(ctx, "k2", "t1"))
	got, _, err = st.acquire(ctx, "k2", "t2", lease)
	is.NoErr(err)
	is.Equal(got, acquired) // a released key is free

	advance(2 * lease)
	got, _, err = st.acquire(ctx, "k2", "t3", lease)
	is.NoErr(err)
	is.Equal(got, acquired) // an expired lease frees the key
	ok, err = st.complete(ctx, "k2", "t2", []byte(`"late"`), ttl)
	is.NoErr(err)
	is.True(!ok) // the holder whose lease ended does not complete

	advance(2 * ttl)
	got, _, err = st.acquire(ctx, "k1", "t6", lease)
	is.NoErr(err)
	is.Equal(got, acquired) // an expired TTL frees the key

	var wg sync.WaitGroup
	results := make(chan state, 32)
	for i := range 32 {
		wg.Go(func() {
			got, _, err := st.acquire(ctx, "k3", fmt.Sprint("c", i), lease)
			if err != nil {
				t.Error(err)
			}
			results <- got
		})
	}
	wg.Wait()
	close(results)
	won := 0
	for got := range results {
		if got == acquired {
			won++
		}
	}
	is.Equal(won, 1) // one of the concurrent calls acquires
}

func TestMemoryStore(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		storeContract(t, newMemory(), time.Sleep)
	})
}

func TestMemorySweep(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		is := is.New(t)
		ctx := context.Background()
		m := newMemory()
		_, _, err := m.acquire(ctx, "old", "t1", time.Second)
		is.NoErr(err)
		time.Sleep(2 * sweepEvery)
		_, _, err = m.acquire(ctx, "new", "t2", time.Second)
		is.NoErr(err)
		is.Equal(len(m.entries), 1) // the sweep dropped the expired record
	})
}

func TestRedisStore(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	storeContract(t, redisStore{client: client}, mr.FastForward)
}

func TestRedisStoreForeignKey(t *testing.T) {
	is := is.New(t)
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	is.NoErr(mr.Set(redisPrefix+"k", "not a record"))
	_, _, err := redisStore{client: client}.acquire(context.Background(), "k", "t", time.Minute)
	is.True(err != nil) // a value dedup did not write is an error
}
