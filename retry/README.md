# retry

Run an operation again on failure — with exponential backoff, optional jitter, a
bounded attempt budget, pluggable delay strategies, early stop on terminal
errors, and a per-retry observation hook.

`retry.Do` wraps **any** context-aware function — an HTTP call, a SQL
transaction, a broker publish, a queue job — so retry policy is composed at the
call site instead of being reimplemented in every subsystem. `retry.Backoff` is
the delay/budget policy, shared across the SDK (the HTTP client middleware,
reliable workers, the queue), so there is one place that decides how retries are
spaced.

## Features

- One entry point — `Do(ctx, Config, fn)` — for any `func(ctx) error`.
- Exponential backoff with cap, factor, and jitter (`Backoff`).
- Fixed delay (`Fixed`) or any custom cadence (`Strategy`).
- Stop early on a critical/terminal error, or on any error (`IsTerminal`).
- Observe every retry for logging/statistics (`OnRetry`).
- Context-aware: cancels promptly during a wait; never loops unboundedly.
- Deterministic in tests: inject `Sleep` and `Rand`.
- No third-party dependencies; the same `Backoff` powers `httpmw` and `outbox`.

## Install

```go
import "github.com/assanoff/skit/retry"
```

## Quick start — exponential backoff

Retry up to 4 times, doubling the delay from 100ms (capped at 2s), with ±20%
jitter:

```go
err := retry.Do(ctx, retry.Config{
    Backoff: retry.Backoff{
        Base: 100 * time.Millisecond, Factor: 2, Max: 2 * time.Second,
        MaxAttempts: 4, Jitter: 0.2,
    },
}, func(ctx context.Context) error {
    return callFlakyService(ctx)
})
if err != nil {
    return fmt.Errorf("service still failing after retries: %w", err)
}
```

`Do` returns `nil` on the first success, or the last error once the budget is
spent. `MaxAttempts <= 1` means a single attempt (no retry).

## Strategies

The delay before each retry comes from `Backoff` by default. Set `Strategy` to
override the cadence; the attempt budget still comes from `Backoff.MaxAttempts`.

### Fixed delay

```go
err := retry.Do(ctx, retry.Config{
    Backoff:  retry.Backoff{MaxAttempts: 5}, // budget only
    Strategy: retry.Fixed(time.Second),       // wait 1s between every attempt
}, func(ctx context.Context) error {
    return poll(ctx)
})
```

### Custom strategy

`Strategy` receives the 1-based number of the attempt that just failed and
returns the wait before the next one — implement any schedule:

```go
// Read delays from a fixed schedule (1s, 5s, 30s, ...), clamping past the end.
schedule := []time.Duration{time.Second, 5 * time.Second, 30 * time.Second}
err := retry.Do(ctx, retry.Config{
    Backoff: retry.Backoff{MaxAttempts: len(schedule) + 1},
    Strategy: func(attempt int) time.Duration {
        if attempt > len(schedule) {
            return schedule[len(schedule)-1]
        }
        return schedule[attempt-1]
    },
}, func(ctx context.Context) error {
    return reconnect(ctx)
})
```

## Stop conditions

### Stop on a critical / terminal error

Some failures will never succeed on retry (bad input, a 4xx). Classify them with
`IsTerminal` to stop immediately and return that error:

```go
var ErrBadInput = errors.New("bad input")

err := retry.Do(ctx, retry.Config{
    Backoff:    retry.Backoff{Base: 200 * time.Millisecond, MaxAttempts: 5},
    IsTerminal: func(err error) bool { return errors.Is(err, ErrBadInput) },
}, func(ctx context.Context) error {
    return process(ctx) // returns ErrBadInput → no retry; anything else → retry
})
```

### Stop on any error (try once, never retry)

```go
err := retry.Do(ctx, retry.Config{
    Backoff:    retry.Backoff{MaxAttempts: 5},
    IsTerminal: func(error) bool { return true }, // every error is terminal
}, fn)
```

(Equivalently, leave `MaxAttempts <= 1` for a single attempt.)

## Observe retries (logging & statistics)

`OnRetry` fires once per retry that will actually happen — after an attempt fails
and before the wait. It is **not** called for a success, a terminal error, or
the final failed attempt:

```go
var attempts atomic.Int64
err := retry.Do(ctx, retry.Config{
    Backoff: retry.Backoff{Base: 100 * time.Millisecond, MaxAttempts: 5},
    OnRetry: func(attempt int, err error) {
        attempts.Add(1)
        log.Warn("retrying", "attempt", attempt, "err", err)
    },
}, func(ctx context.Context) error {
    return callFlakyService(ctx)
})
// attempts.Load() == number of retries performed
```

## Context cancellation

`Do` aborts promptly when `ctx` is cancelled during a backoff wait and returns
the operation's last error. Give the whole retry a deadline with a timeout
context:

```go
ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
defer cancel()
err := retry.Do(ctx, cfg, fn)
```

## Backoff as a delay calculator (distributed / persistent retry)

`Do` is for **in-process** retry. For retry that must survive a crash and not
block a worker — the transactional outbox, a durable queue — you don't run an
in-process loop; you store *when* to try next and let a background worker pick it
up on a later tick. There, use `Backoff.Next(attempt)` as a plain value:

```go
b := retry.Backoff{Base: 2 * time.Second, Factor: 2, Max: 5 * time.Minute, MaxAttempts: 10}

// On failure, schedule the next attempt instead of looping in memory:
nextAttemptAt := now.Add(b.Next(attempts))
if b.Exhausted(attempts) {
    // give up: mark the row failed / dead-letter it
}
```

This is exactly how `outbox` computes `next_attempt_at` and how `httpmw` spaces
its HTTP retries — the same `Backoff`, used as math rather than as a loop.

## Integration with the SDK

### Wrap a worker Handler

`worker.Retry` adds in-process retry to a `worker.Handler[T]` (e.g. a queue
`Mux`), absorbing transient errors before the Processor records the outcome:

```go
handler := worker.Retry[queue.Task](retry.Config{
    Backoff:    retry.Backoff{Base: 50 * time.Millisecond, MaxAttempts: 3},
    IsTerminal: func(err error) bool { return errors.Is(err, ErrPermanent) },
}, mux)

proc := worker.NewProcessor[queue.Task](log, q, handler, q, worker.ProcessorConfig{Name: "queue"})
```

### Wrap a queue JobFunc

```go
mux := queue.NewMux()
if err := mux.Register("email.welcome", queue.Retry(retry.Config{
    Backoff: retry.Backoff{Base: 100 * time.Millisecond, MaxAttempts: 3},
}, sendWelcome)); err != nil {
    return err
}
```

### HTTP client middleware

`httpmw.RetryTransport` takes a `retry.Backoff` and additionally honors a
server's `Retry-After` header — see the `httpmw` package.

## Caveats

- **In-process, blocking.** `Do` runs retries in the calling goroutine and sleeps
  on the delay. When the caller holds a resource for the duration — a queue lease,
  a DB transaction — keep the budget and delays well under that resource's
  timeout, or use the distributed pattern above instead.
- **Budget lives in `Backoff.MaxAttempts`,** even when a custom `Strategy`
  overrides the delay. `MaxAttempts <= 1` ⇒ a single attempt; `0` is treated as
  a single attempt too (`Do` never loops unboundedly).
- **`Rand` is only used by the default `Backoff` cadence.** A custom `Strategy`
  computes its own (deterministic) delay.
