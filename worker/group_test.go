package worker_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/matryer/is"

	"github.com/assanoff/skit/worker"
)

// rec builds a Runnable that records its Start and Stop names into the shared
// slices (guarded by mu). When start is non-nil it runs instead of blocking on
// ctx, so a test can make a brick fail on its own.
func rec(mu *sync.Mutex, started, stopped *[]string, name string, start func(ctx context.Context) error) worker.Runnable {
	return worker.RunnableFunc{
		NameValue: name,
		StartFn: func(ctx context.Context) error {
			mu.Lock()
			*started = append(*started, name)
			mu.Unlock()
			if start != nil {
				return start(ctx)
			}
			<-ctx.Done()
			return ctx.Err()
		},
		StopFn: func(ctx context.Context) error {
			mu.Lock()
			*stopped = append(*stopped, name)
			mu.Unlock()
			return nil
		},
	}
}

func TestGroupAddSkipsNil(t *testing.T) {
	is := is.New(t)

	var (
		mu               sync.Mutex
		started, stopped []string
	)

	g := worker.NewGroup(nil, time.Second)
	// nil entries (a disabled, addr-gated brick) are dropped, not supervised.
	g.Add(rec(&mu, &started, &stopped, "a", nil), nil, rec(&mu, &started, &stopped, "b", nil), nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled: Run starts, sees ctx.Done, stops cleanly
	is.NoErr(g.Run(ctx))

	is.Equal(len(stopped), 2) // only the two non-nil bricks were supervised
}

func TestGroupStopsInReverseOrder(t *testing.T) {
	is := is.New(t)

	var (
		mu               sync.Mutex
		started, stopped []string
	)

	g := worker.NewGroup(nil, time.Second)
	g.Add(
		rec(&mu, &started, &stopped, "a", nil),
		rec(&mu, &started, &stopped, "b", nil),
		rec(&mu, &started, &stopped, "c", nil),
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	is.NoErr(g.Run(ctx))

	is.Equal(stopped, []string{"c", "b", "a"}) // LIFO: last registered stops first
}

func TestGroupReturnsFirstErrorAndStopsAll(t *testing.T) {
	is := is.New(t)

	var (
		mu               sync.Mutex
		started, stopped []string
	)
	boom := errors.New("boom")

	g := worker.NewGroup(nil, time.Second)
	g.Add(
		rec(&mu, &started, &stopped, "ok", nil), // blocks until the group tears down
		rec(&mu, &started, &stopped, "bad", func(ctx context.Context) error {
			return boom
		}),
	)

	err := g.Run(context.Background())
	is.True(errors.Is(err, boom)) // the first triggering error is returned

	is.Equal(len(stopped), 2) // every brick is asked to Stop on teardown
}
