package dedup

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/matryer/is"
	"github.com/redis/go-redis/v9"
)

var errBoom = errors.New("boom")

// faultyStore is a memory store whose operations fail on demand.
type faultyStore struct {
	*memory

	acquireErr  error
	completeErr error
	releaseErr  error
}

func (f faultyStore) acquire(ctx context.Context, key, token string, lease time.Duration) (state, []byte, error) {
	if f.acquireErr != nil {
		return 0, nil, f.acquireErr
	}
	return f.memory.acquire(ctx, key, token, lease)
}

func (f faultyStore) complete(ctx context.Context, key, token string, value []byte, ttl time.Duration) (bool, error) {
	if f.completeErr != nil {
		return false, f.completeErr
	}
	return f.memory.complete(ctx, key, token, value, ttl)
}

func (f faultyStore) release(ctx context.Context, key, token string) error {
	if f.releaseErr != nil {
		return f.releaseErr
	}
	return f.memory.release(ctx, key, token)
}

// counter returns an fn that counts its calls and returns value.
func counter[T any](calls *int, value T) func(context.Context) (T, error) {
	return func(context.Context) (T, error) {
		*calls++
		return value, nil
	}
}

func TestDoRepeatGetsStored(t *testing.T) {
	is := is.New(t)
	k := NewMemory()
	calls := 0

	v, err := k.Do(t.Context(), "k", counter(&calls, 7))
	is.NoErr(err)
	is.Equal(v, 7)
	v, err = k.Do(t.Context(), "k", counter(&calls, 8))
	is.NoErr(err)
	is.Equal(v, 7)     // the repeat gets the stored success
	is.Equal(calls, 1) // fn runs once
}

func TestDoErrorFreesKey(t *testing.T) {
	is := is.New(t)
	k := NewMemory()

	v, err := k.Do(t.Context(), "k", func(context.Context) (int, error) {
		return 3, errBoom
	})
	is.True(errors.Is(err, errBoom)) // the error of fn as it is
	is.Equal(v, 3)                   // with the value of fn

	calls := 0
	_, err = k.Do(t.Context(), "k", counter(&calls, 4))
	is.NoErr(err)
	is.Equal(calls, 1) // the repeat runs fn again
}

func TestDoPanicFreesKey(t *testing.T) {
	is := is.New(t)
	k := NewMemory()

	func() {
		defer func() { is.Equal(recover(), "boom") }() // the panic goes on
		_, _ = k.Do(t.Context(), "k", func(context.Context) (int, error) {
			panic("boom")
		})
	}()

	calls := 0
	_, err := k.Do(t.Context(), "k", counter(&calls, 1))
	is.NoErr(err)
	is.Equal(calls, 1) // the repeat runs fn again
}

func TestDoEncodeErrorFreesKey(t *testing.T) {
	is := is.New(t)
	k := NewMemory()

	_, err := k.Do(t.Context(), "k", func(context.Context) (chan int, error) {
		return make(chan int), nil
	})
	is.True(err != nil) // a success encoding/json cannot encode

	calls := 0
	_, err = k.Do(t.Context(), "k", counter(&calls, 1))
	is.NoErr(err)
	is.Equal(calls, 1) // the repeat runs fn again
}

func TestDoInProgress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		is := is.New(t)
		k := NewMemory()
		ctx := t.Context()
		running, finish := make(chan struct{}), make(chan struct{})

		go func() {
			_, _ = k.Do(ctx, "k", func(context.Context) (int, error) {
				close(running)
				<-finish
				return 1, nil
			})
		}()
		<-running

		calls := 0
		_, err := k.Do(ctx, "k", counter(&calls, 2))
		is.True(errors.Is(err, ErrInProgress)) // at once, without waiting
		is.Equal(calls, 0)                     // fn does not run
		close(finish)
		synctest.Wait()
	})
}

func TestDoUnavailable(t *testing.T) {
	is := is.New(t)
	k := newKeeper(faultyStore{memory: newMemory(), acquireErr: errBoom}, nil, nil)

	calls := 0
	_, err := k.Do(t.Context(), "k", counter(&calls, 1))
	is.True(errors.Is(err, ErrUnavailable)) // the store is down
	is.True(errors.Is(err, errBoom))        // with its error
	is.Equal(calls, 0)                      // fn does not run
}

func TestDoEmptyKey(t *testing.T) {
	is := is.New(t)
	calls := 0
	_, err := NewMemory().Do(t.Context(), "", counter(&calls, 1))
	is.True(err != nil)
	is.Equal(calls, 0)
}

func TestDoDecodeError(t *testing.T) {
	is := is.New(t)
	k := NewMemory()
	calls := 0

	_, err := k.Do(t.Context(), "k", counter(&calls, 1))
	is.NoErr(err)
	_, err = k.Do(t.Context(), "k", counter(&calls, "one"))
	is.True(err != nil) // the stored int does not decode into a string
	is.Equal(calls, 1)
}

func TestDoCompleteFailureReturnsResult(t *testing.T) {
	is := is.New(t)
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	st := faultyStore{memory: newMemory(), completeErr: errBoom}
	k := newKeeper(st, log, nil)

	calls := 0
	v, err := k.Do(t.Context(), "k", counter(&calls, 5))
	is.NoErr(err)
	is.Equal(v, 5) // the caller gets the result of fn
	is.True(strings.Contains(buf.String(), "dedup: result not recorded"))
}

func TestDoReleaseFailureLogs(t *testing.T) {
	is := is.New(t)
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	st := faultyStore{memory: newMemory(), releaseErr: errBoom}
	k := newKeeper(st, log, nil)

	_, err := k.Do(t.Context(), "k", func(context.Context) (int, error) {
		return 0, errBoom
	})
	is.True(errors.Is(err, errBoom))
	is.True(strings.Contains(buf.String(), "dedup: release failed"))
}

func TestDoLeaseIsDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		is := is.New(t)
		k := NewMemory(WithLease(30 * time.Second))
		start := time.Now()

		_, err := k.Do(t.Context(), "k", func(ctx context.Context) (int, error) {
			deadline, ok := ctx.Deadline()
			is.True(ok)
			is.Equal(deadline.Sub(start), 30*time.Second) // fn runs under the lease
			return 1, nil
		})
		is.NoErr(err)
	})
}

func TestDoLeaseLostLogs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		is := is.New(t)
		var buf bytes.Buffer
		log := slog.New(slog.NewTextHandler(&buf, nil))
		k := newKeeper(newMemory(), log, []Option{WithLease(time.Second)})

		v, err := k.Do(t.Context(), "k", func(context.Context) (int, error) {
			time.Sleep(2 * time.Second) // fn ignores its deadline
			return 9, nil
		})
		is.NoErr(err)
		is.Equal(v, 9) // the result of fn still comes back
		is.True(strings.Contains(buf.String(), "dedup: result not recorded"))
	})
}

func TestDoOptionsDefaults(t *testing.T) {
	is := is.New(t)
	k := NewMemory(WithTTL(-time.Second), WithLease(0))
	is.Equal(k.ttl, defaultTTL)
	is.Equal(k.lease, defaultLease)

	k = NewMemory(WithTTL(time.Hour), WithLease(time.Second))
	is.Equal(k.ttl, time.Hour)
	is.Equal(k.lease, time.Second)
}

func TestRedisDo(t *testing.T) {
	is := is.New(t)
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	k := NewRedis(client, nil)

	type result struct {
		ID    string `json:"id"`
		Total int    `json:"total"`
	}
	calls := 0
	_, err := k.Do(t.Context(), "order/1", counter(&calls, result{ID: "a", Total: 2}))
	is.NoErr(err)
	v, err := k.Do(t.Context(), "order/1", counter(&calls, result{ID: "b"}))
	is.NoErr(err)
	is.Equal(v, result{ID: "a", Total: 2}) // the repeat gets the stored success
	is.Equal(calls, 1)
	is.True(mr.TTL(redisPrefix+"order/1") > time.Hour) // stored for the TTL

	mr.Close()
	_, err = k.Do(t.Context(), "order/2", counter(&calls, result{}))
	is.True(errors.Is(err, ErrUnavailable)) // Redis is down
	is.Equal(calls, 1)
}
