package poller

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestPollerStartSeedsAndRefreshes(t *testing.T) {
	var n atomic.Int64
	p := New(nil, -1, func(context.Context) (int64, error) {
		return n.Add(1), nil
	}, Config{Name: "test", Interval: 5 * time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Start(ctx) }()

	// Start polls immediately, so Current should leave the initial value quickly.
	deadline := time.After(time.Second)
	for p.Current() < 3 {
		select {
		case <-deadline:
			t.Fatalf("value did not advance, stuck at %d", p.Current())
		case <-time.After(2 * time.Millisecond):
		}
	}

	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled from Start, got %v", err)
	}
}

func TestPollerKeepsValueOnError(t *testing.T) {
	wantErr := errors.New("source down")
	var gotErr atomic.Pointer[error]

	first := true
	p := New(nil, "initial", func(context.Context) (string, error) {
		if first {
			first = false
			return "fresh", nil
		}
		return "", wantErr
	}, Config{
		Interval: time.Hour, // we drive polls manually
		OnError:  func(err error) { gotErr.Store(&err) },
	})

	p.Poll(context.Background()) // success -> "fresh"
	if got := p.Current(); got != "fresh" {
		t.Fatalf("after success: got %q want fresh", got)
	}

	p.Poll(context.Background()) // error -> value unchanged, OnError fired
	if got := p.Current(); got != "fresh" {
		t.Errorf("after error: value changed to %q, want fresh", got)
	}
	if ep := gotErr.Load(); ep == nil || !errors.Is(*ep, wantErr) {
		t.Errorf("OnError not invoked with %v", wantErr)
	}
}

func TestPollerRecoversPanic(t *testing.T) {
	var panics atomic.Int64
	p := New(nil, 0, func(context.Context) (int, error) {
		panic("boom")
	}, Config{
		Interval: time.Hour,
		OnPanic:  func(string, string) { panics.Add(1) },
	})

	// Must not propagate the panic.
	p.Poll(context.Background())

	if panics.Load() != 1 {
		t.Errorf("expected OnPanic to fire once, got %d", panics.Load())
	}
	if p.Current() != 0 {
		t.Errorf("value should be unchanged after panic, got %d", p.Current())
	}
}
