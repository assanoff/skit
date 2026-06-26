package worker

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPoolBoundsConcurrency(t *testing.T) {
	const limit = 3
	p := NewPool(limit)

	var (
		mu      sync.Mutex
		current int
		peak    int
	)

	// Jobs self-complete after a short overlap window. Submit blocks only until a
	// slot frees, so the submit loop never deadlocks even though it runs more jobs
	// than there are slots.
	for i := range 12 {
		if _, err := p.Submit(context.Background(), func(context.Context) {
			mu.Lock()
			current++
			if current > peak {
				peak = current
			}
			mu.Unlock()

			time.Sleep(5 * time.Millisecond) // keep the slot busy long enough to overlap

			mu.Lock()
			current--
			mu.Unlock()
		}); err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
	}

	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if peak > limit {
		t.Errorf("peak concurrency %d exceeded limit %d", peak, limit)
	}
	if peak == 0 {
		t.Error("no jobs ran")
	}
}

func TestPoolShutdownDrainsAndRejects(t *testing.T) {
	p := NewPool(2)

	var done atomic.Int64
	for i := range 4 {
		if _, err := p.Submit(context.Background(), func(context.Context) {
			time.Sleep(10 * time.Millisecond)
			done.Add(1)
		}); err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
	}

	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if got := done.Load(); got != 4 {
		t.Errorf("expected all 4 jobs to finish, got %d", got)
	}

	// Submitting after shutdown is rejected.
	if _, err := p.Submit(context.Background(), func(context.Context) {}); !errors.Is(err, ErrPoolShutdown) {
		t.Errorf("expected ErrPoolShutdown after shutdown, got %v", err)
	}
}

func TestPoolShutdownCancelsJobContext(t *testing.T) {
	p := NewPool(1)

	started := make(chan struct{})
	canceled := make(chan struct{})
	if _, err := p.Submit(context.Background(), func(ctx context.Context) {
		close(started)
		<-ctx.Done() // Shutdown must cancel this
		close(canceled)
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}

	<-started
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	select {
	case <-canceled:
	default:
		t.Error("job context was not canceled by Shutdown")
	}
}

func TestPoolCancelStopsSingleJob(t *testing.T) {
	p := NewPool(2)

	canceled := make(chan struct{})
	key, err := p.Submit(context.Background(), func(ctx context.Context) {
		<-ctx.Done()
		close(canceled)
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	p.Cancel(key)
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Error("Cancel did not stop the job")
	}
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}
