# auditlog

Versioned audit trail of model changes, with history and diff queries.

Each change is stored as a JSON snapshot keyed by `(model_type, model_id)`.
Snapshots for the same model form an ordered history with a monotonically
increasing `version`. `Create` is idempotent against no-op writes: if a new
snapshot is identical to the latest one (ignoring the volatile `updated_at` /
`updated_by` fields) no new version is stored.

```
auditlog/             core: Core, Store, models, QueryFilter, Recorder, Compact, AsyncRecorder
├── db/               Postgres implementation of Store (+ Schema/EnsureSchema)
├── auditbus/         eventbus adapter — record over the in-process (or external) bus
├── auditqueue/       durable, queue-backed Recorder + Worker
├── auditrest/        read-side handler group (history / diff / changes)
└── mocks/            generated Store mock (moq)
```

## Design: audit at the domain layer

Audit a **business state change**, not "an HTTP request happened". Emitting the
change in the domain — not in transport middleware — means every path (REST, gRPC,
background workers, queue consumers, other services) is covered by one wiring, and
the audit can commit atomically with the business write. There is intentionally no
HTTP middleware / gRPC interceptor.

## Setup

```go
store := auditdb.NewStore(log, db)      // db is *sqlx.DB
core  := auditlog.NewCore(log, store)

// Tests / simple boot: EnsureSchema runs the DDL under a transaction-level
// advisory lock, so concurrent replica startups don't race. In production prefer
// a migration — goose already serializes migrations with its own advisory lock.
_ = store.EnsureSchema(ctx)
```

The schema declares `UNIQUE (model_type, model_id, version)`. Because `Create` is
a read-modify-write, this makes concurrent writers safe: a racing insert at the
same version fails, and `Create` re-reads and retries (bounded) so concurrent
distinct changes are not lost.

## Recording a change

All paths converge on `Core.Create`. Pick the entry point per call site:

| Way                       | How                                                           | Delivery |
| ------------------------- | ------------------------------------------------------------ | -------- |
| Direct                    | `core.Create(ctx, auditlog.NewAuditLog{...})`                | caller-controlled; atomic via `store.WithTx(tx)` |
| Internal bus (decoupled)  | `auditbus.Register(bus, rec)` + `auditbus.Publish(ctx, bus, ev)` | best-effort |
| External bus              | decode a broker delivery → `auditbus.Record(ctx, core, ev)`  | caller-controlled (ack/nack) |

```go
// Direct, atomic with the business change:
err := dbx.WithinTran(ctx, log, db, func(tx *sqlx.Tx) error {
    if err := widgetStore.WithTx(tx).Update(ctx, w); err != nil { return err }
    _, err := auditlog.NewCore(log, store.WithTx(tx)).Create(ctx, auditlog.NewAuditLog{
        ModelType: "widget", ModelID: w.ID, CreatedBy: actor(ctx), Payload: w,
    })
    return err
})

// Decoupled over the in-process bus — the producer never imports auditlog:
auditbus.Register(bus, core)
auditbus.Publish(ctx, bus, auditbus.Event{ModelType: "widget", ModelID: id, CreatedBy: actor, Payload: raw})
```

## Recorders (sync vs async)

Producers depend on the `Recorder` interface (`Record(ctx, NewAuditLog)`), so the
write can be moved off the request path without changing call sites:

| Recorder              | Latency | Durability      | Ordering |
| --------------------- | ------- | --------------- | -------- |
| `*Core` (sync)        | DB write inline | as durable as the tx (atomic possible) | strict |
| `auditlog.AsyncRecorder` | non-blocking | lost on crash | per-model (sharded) |
| `auditqueue.Recorder` | ~1 INSERT | durable (survives crash) | not guaranteed across consumers |

```go
// In-process, non-blocking, per-model order preserved:
rec := auditlog.NewAsyncRecorder(core, auditlog.AsyncConfig{Workers: 4, Buffer: 512})
group.Add(rec)                  // worker.Runnable
auditbus.Register(bus, rec)     // bus consumer now records asynchronously

// Durable queue + worker:
qrec := auditqueue.NewRecorder(q, log)
group.Add(auditqueue.NewWorker(q, core, log, auditqueue.WorkerConfig{Interval: time.Second}))
```

## Read API (handler group)

Mount the query endpoints on any skit router in one call:

```go
auditrest.NewHandlers(core).Routes(r)              // or Routes(r, adminOnly)
```

| Method & path                                                 | Returns |
| ------------------------------------------------------------- | ------- |
| `GET /auditlog/{model_type}/{model_id}`                       | full version history |
| `GET /auditlog/{model_type}/{model_id}/diff?current=&target=` | diff between two versions (`base64=true` to encode) |
| `GET /auditlog/{model_type}/{model_id}/changes`               | each version with its changed fields |

## Compaction

Thin history while always keeping the first (baseline) and last (current) version.
Two API entry points — **scheduling is the caller's job via the worker package**
(auditlog ships no built-in loop):

```go
// 1. Opportunistic, inline on write — compacts the model just written once every
//    N versions (best-effort, gated so most writes pay nothing). Often enough on
//    its own.
core := auditlog.NewCore(log, store, auditlog.WithAutoCompact(100, opts))

// 2. On demand, in portions — from an API handler, a CLI command, or a worker.
//    Loop until res.Models == 0 to drain a large backlog gently.
res, err := core.CompactBatch(ctx, auditlog.CompactBatchOptions{
    Threshold: 100, Limit: 50, Compact: opts,
})

// single model:
deleted, err := core.Compact(ctx, "widget", id, opts)
```

Run `CompactBatch` on a schedule with the worker package:

```go
tick := func(ctx context.Context) error {
    _, err := core.CompactBatch(ctx, auditlog.CompactBatchOptions{
        Threshold: 100, Limit: 50, Compact: opts,
    })
    return err
}
group.Add(worker.NewLoop(log.Slog(), worker.LoopConfig{
    Name: "auditlog-compaction", Interval: time.Hour,
}, tick))
```

`CompactOptions`: `Factor` keeps every N-th middle version; `MaxVersions` caps the
total (even downsample); `KeepRecent` always retains the newest N; `MinVersions`
skips small histories. First and last are never removed.

## Limitations

- The field-level `changes` diff compares scalar fields only; array changes and
  removed keys are not reported. The textual `diff` (go-cmp) covers everything.
- History is returned unpaginated.
- `auditqueue` does not guarantee per-model ordering across consumers — use a
  single consumer or `AsyncRecorder` when strict version ordering matters.
