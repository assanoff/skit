package retry

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

// noSleep is an injected Sleep that never waits but still honors cancellation,
// so tests run instantly and deterministically.
func noSleep(ctx context.Context, _ time.Duration) error { return ctx.Err() }

func TestDoSucceedsFirstTry(t *testing.T) {
	calls := 0
	err := Do(context.Background(), Config{
		Backoff: Backoff{MaxAttempts: 5},
		Sleep:   noSleep,
	}, func(context.Context) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestDoRetriesThenSucceeds(t *testing.T) {
	calls := 0
	err := Do(context.Background(), Config{
		Backoff: Backoff{Base: time.Millisecond, MaxAttempts: 5},
		Sleep:   noSleep,
	}, func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestDoExhaustsBudget(t *testing.T) {
	want := errors.New("always")
	calls := 0
	err := Do(context.Background(), Config{
		Backoff: Backoff{Base: time.Millisecond, MaxAttempts: 3},
		Sleep:   noSleep,
	}, func(context.Context) error {
		calls++
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3 (budget)", calls)
	}
}

func TestDoTerminalStopsEarly(t *testing.T) {
	terminal := errors.New("permanent")
	calls := 0
	err := Do(context.Background(), Config{
		Backoff:    Backoff{Base: time.Millisecond, MaxAttempts: 10},
		IsTerminal: func(err error) bool { return errors.Is(err, terminal) },
		Sleep:      noSleep,
	}, func(context.Context) error {
		calls++
		return terminal
	})
	if !errors.Is(err, terminal) {
		t.Fatalf("err = %v, want %v", err, terminal)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (terminal stops at once)", calls)
	}
}

func TestDoSingleAttemptWhenBudgetUnset(t *testing.T) {
	calls := 0
	err := Do(context.Background(), Config{Sleep: noSleep}, func(context.Context) error {
		calls++
		return errors.New("boom")
	})
	if err == nil {
		t.Fatal("err = nil, want error")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (no budget = single attempt)", calls)
	}
}

func TestDoStrategyOverridesBackoffAndOnRetryObserves(t *testing.T) {
	boom := errors.New("boom")
	var delays []time.Duration
	var retries []int
	calls := 0

	err := Do(context.Background(), Config{
		Backoff:  Backoff{MaxAttempts: 3}, // budget only; delay comes from Strategy
		Strategy: func(attempt int) time.Duration { return time.Duration(attempt) * time.Second },
		OnRetry:  func(attempt int, _ error) { retries = append(retries, attempt) },
		Sleep: func(_ context.Context, d time.Duration) error {
			delays = append(delays, d)
			return nil
		},
	}, func(context.Context) error {
		calls++
		return boom
	})

	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3 (budget)", calls)
	}
	// Two retries happen (after attempts 1 and 2); the 3rd attempt is the last.
	if want := []time.Duration{time.Second, 2 * time.Second}; !slices.Equal(delays, want) {
		t.Fatalf("strategy delays = %v, want %v", delays, want)
	}
	if want := []int{1, 2}; !slices.Equal(retries, want) {
		t.Fatalf("OnRetry attempts = %v, want %v", retries, want)
	}
}

func TestFixedStrategyIsConstant(t *testing.T) {
	s := Fixed(250 * time.Millisecond)
	for _, attempt := range []int{1, 2, 7} {
		if got := s(attempt); got != 250*time.Millisecond {
			t.Errorf("Fixed at attempt %d = %s, want 250ms", attempt, got)
		}
	}
}

func TestDoOnRetryNotCalledOnTerminal(t *testing.T) {
	terminal := errors.New("permanent")
	called := 0
	_ = Do(context.Background(), Config{
		Backoff:    Backoff{Base: time.Millisecond, MaxAttempts: 5},
		IsTerminal: func(err error) bool { return errors.Is(err, terminal) },
		OnRetry:    func(int, error) { called++ },
		Sleep:      noSleep,
	}, func(context.Context) error {
		return terminal
	})
	if called != 0 {
		t.Fatalf("OnRetry called %d times, want 0 on a terminal error", called)
	}
}

func TestDoStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	want := errors.New("transient")
	calls := 0
	err := Do(ctx, Config{
		Backoff: Backoff{Base: time.Hour, MaxAttempts: 10},
		// Sleep cancels the context, simulating a deadline hit mid-wait.
		Sleep: func(_ context.Context, _ time.Duration) error {
			cancel()
			return context.Canceled
		},
	}, func(context.Context) error {
		calls++
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want last operation error %v", err, want)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (cancelled during first backoff)", calls)
	}
}
