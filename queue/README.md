# queue

Durable background work with **at-least-once** delivery, safe for concurrent
workers across goroutines and replicas.

Every queue lives in one Postgres table, `queue_tasks`; the `queue` column tells
them apart. A queue has its own workers, lease and backoff: give slow and fast
work queues of their own, so a long task does not hold up a quick one. Within a
queue, `Kind` selects the handler: a `Mux` routes each task to the `JobFunc`
registered for its kind.

## Features

- Replica-safe claiming via `FOR UPDATE SKIP LOCKED`, a batch in `run_at` order.
- Queues isolated by name: their own workers, lease, backoff and metrics.
- `Key` for idempotent enqueue within a queue; `Delay` to postpone a task.
- Scheduling in the transaction of a domain change: a `Registry` routes a typed
  value to its queue and kind, so the domain never names a queue.
- Outcomes by the error of the handler: done, retry with backoff, `Permanent`
  (dead letter), `Halt` (stop the batch).
- A panic in a handler is recovered per task and retried, or decided by
  `WithOnPanic`.
- A crashed worker's task is reclaimed after its lease (at-least-once).

## Setup

```go
q := queue.NewPG(log, db)
if err := q.EnsureSchema(ctx); err != nil { // safe at startup, even across replicas
    return err
}
```

`EnsureSchema` creates the table and its indexes under an advisory lock, so
several replicas can call it at once.

## Producing work

```go
payload, _ := json.Marshal(WelcomeEmail{UserID: 42})

inserted, err := q.Schedule(ctx, queue.Params{
    Queue:   "email",          // required
    Kind:    "email.welcome",  // required: the handler within the queue
    Key:     "welcome:42",     // optional: dedup within the queue
    Payload: payload,
    Delay:   time.Minute,      // optional: not before now + Delay
})
```

While a task with the same `Key` is in the queue — waiting, leased or dead —
`Schedule` inserts nothing and returns `inserted == false`. An empty `Key`
always inserts.

## Scheduling in a transaction

A task that follows a domain change is scheduled in the transaction of that
change: it never runs for a change that rolled back, and is never lost for one
that committed. A `Registry` routes the Go type of a value to the queue and kind
of its task; `Bind` binds it to the transaction:

```go
reg := queue.NewRegistry()
if err := reg.Register[WelcomeEmail]("email", "email.welcome"); err != nil {
    return err
}
if err := reg.Check("email", mux); err != nil { // every kind has a handler
    return err
}

// In the store, within the transaction of the domain change:
err := queue.Bind(tx, q, reg).Schedule(ctx, WelcomeEmail{UserID: 42})
```

The domain declares its own interface and names neither the queue nor the
transaction:

```go
type Scheduler interface {
    Schedule(ctx context.Context, v any) error
}
```

- `T` and `*T` resolve the same route; an unregistered type is an error.
- The value goes into the `Payload` as JSON; `WithEncoder` sets another
  `Encoder`.
- `Drop()` turns a route off: `Schedule` writes nothing and returns nil, so the
  domain does not branch when an integration is off.
- `Check`, called at startup with the `Mux` of each queue, reports the kinds the
  `Registry` sends there with no handler of their own — the fallback does not
  count — whose tasks would go to the dead letters.

`Key` and `Delay` are for `PG.Schedule` on the pool.

## Consuming work

```go
func sendWelcome(ctx context.Context, t queue.Task, p WelcomeEmail) error {
    return mailer.Send(ctx, p)
}

mux := queue.NewMux()
if err := mux.Register("email.welcome", queue.Decode(sendWelcome)); err != nil {
    return err
}
workers, err := queue.Workers(log, q, queue.Config{
    Queue:       "email",
    Concurrency: 2,                // claim loops per process (default 1)
    Batch:       10,               // tasks per claim (default 1)
    Timeout:     30 * time.Second, // per task (default 30s)
    Poll:        time.Second,      // wait after an empty claim (default 1s)
}, mux.Handle, queue.WithMetrics(reg))
if err != nil {
    return err
}
for _, w := range workers {
    group.Add(w) // a worker.Group
}
```

`Workers` returns the claim loops of the queue, the cleaner of its dead letters
and, with `WithMetrics`, the loop of its backlog gauges. Run them on every
replica: the claim splits the tasks.

`Decode` decodes the JSON payload into the type of the handler; a payload that
does not decode is `Permanent`.

## Outcomes

| Handler returns | The task |
|---|---|
| `nil` | is removed |
| `queue.Permanent(err)` | goes to the dead letters of its queue |
| `queue.Halt(err)` | is retried with backoff; the batch stops, the rest goes back untouched |
| any other error | is retried with backoff |

Use `Halt` for a dependency every task needs, such as a broker that is down:
one failed task per poll instead of a whole batch. A kind with no handler fails
with `ErrUnknownKind`, marked `Permanent`; `WithFallback` handles such kinds
instead.

A retried task runs again after a backoff of 2s × 2 up to 5 minutes, with no
limit on attempts. `WithBackoff` changes it; its `MaxAttempts` sends the task to
the dead letters at that many failures:

```go
queue.WithBackoff(retry.Backoff{Base: time.Second, Max: time.Minute, MaxAttempts: 10})
```

`Task.Attempts` counts only the failures: a task released from a stopped batch,
or reclaimed after a crash, keeps its attempts.

A panic in a handler is recovered and logged with its stack; by default the
task is retried. `WithOnPanic` decides the outcome instead:

```go
queue.WithOnPanic(func(ctx context.Context, t queue.Task, p *safetick.PanicError) error {
    return queue.Permanent(p) // a task that panics once is not run again
})
```

## Delivery

A task stays leased for `3 × Timeout + 10s`. If its worker dies, another
reclaims it after that, so delivery is **at-least-once** and handlers must be
idempotent — upsert with `ON CONFLICT DO NOTHING`, or dedup on a business key.

A batch starts no task once it has run `Timeout`, so no task is handled past
its lease. At shutdown the task being handled is still marked, and the rest of
the batch goes back untouched.

A handled task is removed at once. A dead one stays 7 days for inspection;
then the cleaner of its queue removes it.

## Metrics

With `WithMetrics`, labeled with the queue:

- `skit_queue_handled_total{queue,kind,result}` — `result` is `ok` or `error`;
  a panic is an `error`;
- `skit_queue_handle_duration_seconds{queue,kind}`;
- `skit_queue_pending{queue}` (ready to claim), `skit_queue_leased{queue}` and
  `skit_queue_dead{queue}` — read every 30 seconds. Every replica reports the
  same values, so a dashboard takes `max`, not `sum`.

The loops record `skit_worker_*` under the names `queue-<queue>`,
`queue-<queue>-cleaner` and `queue-<queue>-backlog`. The queue opens no spans.

See also: [`worker`](../worker) (Processor, Loop, Group), [`retry`](../retry)
(the backoff policy).
