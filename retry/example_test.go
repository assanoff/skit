package retry_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/assanoff/skit/retry"
)

// Do runs fn again after each failure, waiting the backoff between attempts,
// until fn succeeds or the budget is spent.
func Example() {
	calls := 0
	err := retry.Do(context.Background(), retry.Config{
		Backoff: retry.Backoff{Base: time.Millisecond, MaxAttempts: 4},
		OnRetry: func(attempt int, err error) {
			fmt.Printf("attempt %d failed: %v\n", attempt, err)
		},
	}, func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("connection refused")
		}
		return nil
	})
	fmt.Println("err:", err, "calls:", calls)
	// Output:
	// attempt 1 failed: connection refused
	// attempt 2 failed: connection refused
	// err: <nil> calls: 3
}

// IsTerminal stops at an error a retry cannot fix.
func ExampleDo_terminal() {
	errBadInput := errors.New("bad input")
	calls := 0
	err := retry.Do(context.Background(), retry.Config{
		Backoff:    retry.Backoff{Base: time.Millisecond, MaxAttempts: 5},
		IsTerminal: func(err error) bool { return errors.Is(err, errBadInput) },
	}, func(context.Context) error {
		calls++
		return errBadInput
	})
	fmt.Println("err:", err, "calls:", calls)
	// Output:
	// err: bad input calls: 1
}

// The delay doubles from Base and stops at Max.
func ExampleBackoff_Next() {
	b := retry.Backoff{Base: 100 * time.Millisecond, Factor: 2, Max: time.Second}
	for attempt := 1; attempt <= 5; attempt++ {
		fmt.Println(attempt, b.Next(attempt))
	}
	// Output:
	// 1 100ms
	// 2 200ms
	// 3 400ms
	// 4 800ms
	// 5 1s
}

// Fixed spaces the retries evenly; the budget still comes from Backoff.
func ExampleFixed() {
	var waits []time.Duration
	_ = retry.Do(context.Background(), retry.Config{
		Backoff:  retry.Backoff{MaxAttempts: 3},
		Strategy: retry.Fixed(250 * time.Millisecond),
		Sleep: func(_ context.Context, d time.Duration) error {
			waits = append(waits, d)
			return nil
		},
	}, func(context.Context) error { return errors.New("busy") })
	fmt.Println(waits)
	// Output:
	// [250ms 250ms]
}
