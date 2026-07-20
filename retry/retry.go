package retry

import (
	"context"
	"math/rand/v2"
	"time"
)

// Config tunes Do: how long to wait between attempts, how many attempts to make,
// which errors stop early, what to observe on each retry, and (for tests) how to
// sleep and how to draw jitter.
type Config struct {
	// Backoff sets the default delay schedule and — always — the attempt budget
	// via its MaxAttempts field. MaxAttempts <= 1 means a single attempt (no
	// retry); set it to >= 2 to retry. Do never loops unboundedly even when
	// MaxAttempts is 0.
	Backoff Backoff
	// Strategy, when set, overrides the delay before each retry: it is called
	// with the 1-based number of the attempt that just failed and returns the
	// wait before the next attempt. Use it for a fixed delay (see Fixed) or any
	// custom cadence. The attempt budget still comes from Backoff.MaxAttempts.
	Strategy func(attempt int) time.Duration
	// IsTerminal, when it returns true for an error, stops retrying and returns
	// that error at once (a critical/permanent error). nil means every error is
	// retryable until the budget is spent; return true unconditionally to stop on
	// any error.
	IsTerminal func(err error) bool
	// OnRetry, when set, is called after an attempt fails and before Do waits to
	// retry — once per retry that actually happens. It is not called for a
	// success, a terminal error, or the final failed attempt (no retry follows
	// those). Use it for logging or retry statistics. attempt is the 1-based
	// number of the attempt that just failed.
	OnRetry func(attempt int, err error)
	// Sleep waits d or returns early with ctx.Err() if ctx is canceled first.
	// Defaults to a context-aware timer; override it in tests for determinism.
	Sleep func(ctx context.Context, d time.Duration) error
	// Rand returns the jitter fraction in [0,1) for each delay. Defaults to
	// math/rand/v2; override it in tests. Consulted only by the default Backoff
	// cadence (a custom Strategy computes its own delay).
	Rand func() float64
}

// Fixed returns a Strategy that always waits d, regardless of the attempt
// number — a constant inter-attempt delay.
func Fixed(d time.Duration) func(attempt int) time.Duration {
	return func(int) time.Duration {
		return d
	}
}

// Do calls fn and retries it on error according to cfg, until fn succeeds, the
// attempt budget is spent, cfg.IsTerminal matches, or ctx is canceled. It
// returns nil on the first success, or the last error otherwise.
//
// Do wraps any context-aware operation — an HTTP call, a DB transaction, a
// broker publish, a queue job — so retry policy is composed at the call site and
// not baked into each subsystem. Because retries run in the calling goroutine
// (blocking on the delay), keep the budget and delays modest when the caller
// holds a resource for the duration (for example a queue lease, which another
// consumer may reclaim if the total retry time exceeds the lease timeout).
func Do(ctx context.Context, cfg Config, fn func(ctx context.Context) error) error {
	budget := cfg.Backoff.MaxAttempts
	if budget < 1 {
		budget = 1
	}
	sleep := cfg.Sleep
	if sleep == nil {
		sleep = sleepCtx
	}
	randFn := cfg.Rand
	if randFn == nil {
		randFn = rand.Float64
	}
	delay := cfg.Strategy
	if delay == nil {
		delay = func(attempt int) time.Duration {
			return cfg.Backoff.NextWithRand(attempt, randFn())
		}
	}

	var err error
	for attempt := 1; attempt <= budget; attempt++ {
		if err = fn(ctx); err == nil {
			return nil
		}
		if cfg.IsTerminal != nil && cfg.IsTerminal(err) {
			return err
		}
		if attempt == budget {
			return err
		}
		if cfg.OnRetry != nil {
			cfg.OnRetry(attempt, err)
		}
		if serr := sleep(ctx, delay(attempt)); serr != nil {
			// Context canceled mid-wait: stop and report the operation's last error.
			return err
		}
	}
	return err
}

// sleepCtx waits for d, returning early with ctx.Err() if ctx is canceled. A
// non-positive d returns immediately (respecting an already-canceled ctx).
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
