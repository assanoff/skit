// Package retry runs an operation again on failure, with exponential backoff,
// optional jitter, a bounded attempt budget, and an early stop on terminal
// errors.
//
// Do is the universal entry point: it wraps any context-aware function — an HTTP
// call, a SQL transaction, a broker publish, a queue job — so retry policy is
// composed at the call site instead of being reimplemented in each subsystem.
// Backoff is the delay/budget policy and is shared across the SDK (the HTTP
// client middleware, reliable workers, this package), so there is one place that
// decides how retries are spaced.
//
// # Usage
//
//	err := retry.Do(ctx, retry.Config{
//	    Backoff: retry.Backoff{
//	        Base: 100 * time.Millisecond, Factor: 2, Max: 2 * time.Second, MaxAttempts: 4,
//	    },
//	    IsTerminal: func(err error) bool { return errors.Is(err, ErrBadInput) },
//	}, func(ctx context.Context) error {
//	    return callFlakyService(ctx)
//	})
//
// # Semantics
//
// Do calls fn, and on a non-nil error waits Backoff.Next(attempt) and tries
// again, until fn succeeds, the attempt budget is spent, IsTerminal matches, or
// ctx is cancelled. It returns nil on the first success or the last error
// otherwise. Retries run in the calling goroutine and block on the backoff
// delay, so keep the budget and delays modest when the caller holds a resource
// for the duration (for example a queue lease, which another consumer may
// reclaim if the total retry time exceeds the lease timeout).
//
// # Config
//
//   - Backoff: inter-attempt delay (Base/Factor/Max/Jitter) and the attempt
//     budget (MaxAttempts). MaxAttempts <= 1 means a single attempt (no retry);
//     Do never loops unboundedly even when MaxAttempts is 0.
//   - Strategy: override the delay cadence per attempt (e.g. Fixed for a
//     constant delay, or any custom schedule); the budget still comes from
//     Backoff.MaxAttempts.
//   - IsTerminal: classify an error as permanent to stop early (nil = every
//     error is retryable until the budget is spent; return true unconditionally
//     to stop on any error).
//   - OnRetry: observe each retry (attempt number + error) for logging or
//     statistics; called only when a retry will actually happen.
//   - Sleep, Rand: seams overridden in tests for deterministic timing and
//     jitter; both default to real implementations.
//
// See the package README for worked examples of each case.
package retry
