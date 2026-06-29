# queue

A durable work queue with **at-least-once** delivery, safe for concurrent
consumers across goroutines and replicas.

The Postgres implementation claims work with `SELECT … FOR UPDATE SKIP LOCKED`,
so running N consumers over one table just works — each ready task goes to
exactly one consumer at a time. A queue plugs straight into `worker.Processor`
(it is both the `Source` and the `Sink`), and a `Mux` routes each task to a
handler by `Kind`, so one queue and one worker pool can serve many task kinds.

> Need to enqueue a task **atomically with a domain write** (the transactional
> outbox guarantee)? `Schedule` is a standalone insert — use the `outbox` package
> for transactional event emission. `queue` is for general deferred/background
> work.

## Features

- Durable, replica-safe claiming via `FOR UPDATE SKIP LOCKED`.
- Lease + attempt tracking; a crashed consumer's task is reclaimed after the
  lease expires (at-least-once).
- Optional `Name` for idempotent enqueue (dedup).
- `Delay` to postpone a task.
- `Mux` dispatch by `Kind` — many job types over one table.
- Two-tier retry: fast **in-process** retry (`queue.Retry`) and durable
  **store** retry (`Options.RetryDelay`), plus dead-lettering for permanent
  failures.
- `InMem` implementation with identical semantics for unit tests.

## Install

```go
import "github.com/assanoff/skit/queue"
```

## Setup

```go
log := logger.New(os.Stdout, logger.Config{Service: "worker"})

q := queue.NewPG(log, db, queue.Options{
    LeaseTimeout: 5 * time.Minute,  // a claimed task is reclaimable after this if not acked
    RetryDelay:   30 * time.Second, // wait before a retryable failure is retried
})
```

Create the table from a migration with `queue.Schema()` (the DDL for the fixed
`queue_tasks` table and its index). In tests or local runs, call
`q.EnsureSchema(ctx)` to run the same DDL directly.

## Producing work

`Schedule` enqueues a task. `Kind` routes it to a handler; `Payload` is opaque
bytes the handler decodes.

```go
payload, _ := json.Marshal(WelcomeEmail{UserID: 42})

inserted, err := q.Schedule(ctx, queue.ScheduleParams{
    Kind:    "email.welcome",
    Payload: payload,
})
```

### Idempotent enqueue (dedup by Name)

A non-empty `Name` is unique: scheduling the same `Name` twice is a no-op and
returns `inserted == false`. An empty `Name` always inserts.

```go
inserted, err := q.Schedule(ctx, queue.ScheduleParams{
    Name:    "import:2024-06",      // dedup key — enqueue this batch at most once
    Kind:    "widget.import",
    Payload: batch,
})
if err != nil {
    return err
}
if !inserted {
    // already queued earlier; nothing to do
}
```

### Delayed work

```go
q.Schedule(ctx, queue.ScheduleParams{
    Kind:    "reminder",
    Payload: b,
    Delay:   24 * time.Hour, // not claimable until now + Delay
})
```

## Consuming work

A `Mux` maps each `Task.Kind` to a `JobFunc` and implements
`worker.Handler[Task]`. Wire it into a `worker.Processor` (the queue is both the
`Source` and `Sink`), run it as a `worker.Loop`, and supervise it in a
`worker.Group`.

```go
func sendWelcome(ctx context.Context, t queue.Task) error {
    var p WelcomeEmail
    if err := json.Unmarshal(t.Payload, &p); err != nil {
        return fmt.Errorf("%w: %v", ErrBadPayload, err) // terminal → dead-letter
    }
    return mailer.Send(ctx, p)
}

func run(ctx context.Context, log *logger.Logger, q *queue.PG) error {
    mux := queue.NewMux()
    if err := mux.Register("email.welcome", sendWelcome); err != nil {
        return err
    }
    if err := mux.Register("widget.import", importBatch); err != nil {
        return err
    }

    proc := worker.NewProcessor[queue.Task](log.Slog(), q, mux, q, worker.ProcessorConfig{
        Name:      "jobs",
        BatchSize: 100,
        // An unroutable kind or a bad payload is permanent — dead-letter it
        // instead of retrying forever.
        IsTerminal: func(err error) bool {
            return errors.Is(err, queue.ErrUnknownKind) || errors.Is(err, ErrBadPayload)
        },
    })

    group := worker.NewGroup(log.Slog(), 10*time.Second)
    group.Add(worker.NewLoop(log.Slog(), worker.LoopConfig{
        Name:               "jobs",
        Interval:           time.Second,
        ImmediateFirstTick: true,
    }, proc.Tick()))

    return group.Run(ctx)
}
```

`Claim` does **not** filter by `Kind`, so the `Mux` is what makes the `Kind`
column route work. To run a single kind you can register just one handler (or
hand a `worker.HandlerFunc[queue.Task]` to the processor directly).

### Unknown kinds

By default a task whose `Kind` has no registered handler yields
`queue.ErrUnknownKind` (classify it terminal, as above, so it dead-letters).
Override with a fallback to handle them yourself:

```go
mux := queue.NewMux(queue.WithFallback(func(ctx context.Context, t queue.Task) error {
    log.Warn(ctx, "dropping task with unknown kind", "kind", t.Kind)
    return nil // ack and drop
}))
```

### High throughput: adaptive pacing

Use a paced loop to drain a backlog promptly while idling cheaply when empty:

```go
group.Add(worker.NewPacedLoop(log.Slog(), worker.LoopConfig{
    Name:            "jobs",
    Interval:        time.Second,      // idle wait
    MaxIdleInterval: 30 * time.Second, // back off when persistently idle
}, proc.PacedTick()))
```

Run several loops (in one process, or across replicas) over the same queue for
more throughput — `SKIP LOCKED` keeps them from colliding.

## Retry & failure handling

There are two complementary tiers.

### In-process retry (fast, transient errors)

`queue.Retry` wraps a `JobFunc` so transient failures retry in-process before the
task is marked failed — no DB round-trip, no poll wait. Keep the budget small;
the task's lease is held for the whole retry.

```go
mux.Register("email.welcome", queue.Retry(retry.Config{
    Backoff:    retry.Backoff{Base: 100 * time.Millisecond, MaxAttempts: 3},
    IsTerminal: func(err error) bool { return errors.Is(err, ErrBadPayload) },
}, sendWelcome))
```

### Store retry (durable, survives crashes)

When a handler returns a non-terminal error, the queue reschedules the task to
`now + Options.RetryDelay` and a later `Claim` retries it — durable, non-blocking,
and replica-safe. Tune it with `Options.RetryDelay`.

### Dead-lettering permanent failures

A failure classified terminal by the processor's `IsTerminal` is parked as a
dead letter (`done_at` set): it is never claimed again but stays in the table for
inspection until `Cleanup` reaps it. Return a terminal sentinel for failures that
will never succeed (bad payload, unknown kind).

```go
var ErrBadPayload = errors.New("bad payload") // classified terminal in ProcessorConfig
```

## Crash recovery (at-least-once)

If a consumer dies after claiming a task but before acking, its lease expires
after `LeaseTimeout` and the next `Claim` re-leases the task (its `Attempts`
increment). Because delivery is at-least-once, **handlers must be idempotent** —
e.g. upsert with `ON CONFLICT DO NOTHING`, or dedup on a business key.

## Retention cleanup

Done and dead-lettered rows accumulate; reap them periodically with a
`worker.Loop`:

```go
group.Add(worker.NewLoop(log.Slog(), worker.LoopConfig{
    Name:     "queue-cleanup",
    Interval: time.Hour,
}, func(ctx context.Context) error {
    _, err := q.Cleanup(ctx, time.Now().Add(-24*time.Hour)) // delete rows finished > 24h ago
    return err
}))
```

## Testing with InMem

`NewInMem` has the same lease/retry/dedup semantics as Postgres but needs no
database — ideal for unit tests. It takes the same `Options` as `NewPG`.

```go
q := queue.NewInMem(queue.Options{LeaseTimeout: time.Minute})

q.Schedule(ctx, queue.ScheduleParams{Kind: "x", Payload: b})
tasks, _ := q.Claim(ctx, time.Now(), 10)
// ... assert on tasks, then q.MarkDone / q.MarkFailed
```

`InMem` is safe for concurrent use but is not durable and does not coordinate
across processes.

## Options reference

| Option | Default | Meaning |
|--------|---------|---------|
| `LeaseTimeout` | 5m | How long a claimed task stays leased before another consumer may reclaim it (crash recovery). |
| `RetryDelay` | 30s | How long a retryable (non-terminal) failure waits before it is claimable again. |

`ScheduleParams`: `Name` (optional dedup key — empty means always insert),
`Kind` (handler routing key, required), `Payload` (opaque bytes), `Delay`
(postpones the earliest claim time).

## Semantics summary

- **Claim** leases up to `limit` ready tasks (`run_at <= now`, unleased or
  lease-expired), bumps each task's `Attempts`, and stamps a fresh `LeaseID`.
- **MarkDone** acks success and removes the task.
- **MarkFailed** (terminal=false) reschedules to `now + RetryDelay`;
  (terminal=true) parks a dead letter.
- A mark with a stale lease (the task was reclaimed) is a no-op returning
  `ErrLeaseLost`.
- All time math and lease-id generation happen in Go, so the SQL stays free of
  `NOW()`/`gen_random_uuid()` and the clock is injectable in tests.

See also: [`worker`](../worker) (Processor/Loop/Group), [`retry`](../retry)
(in-process retry policy), [`outbox`](../outbox) (transactional event emission).
