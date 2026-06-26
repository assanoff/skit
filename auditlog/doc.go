// Package auditlog records a versioned audit trail of model changes and answers
// history and diff queries over it.
//
// Each change is stored as a snapshot keyed by (ModelType, ModelID); snapshots
// for the same model form an ordered history with a monotonically increasing
// Version. Create is idempotent against no-op writes: if the new snapshot is
// identical to the latest one (ignoring updated_at/updated_by) no new version is
// stored. The Core depends only on the Store interface declared here; the
// Postgres implementation lives in the db subpackage.
//
// # Recording changes
//
// Audit at the domain layer, not the transport: emit a change where it happens,
// so every path (REST, gRPC, workers, consumers) is covered by one wiring. Two
// equivalent entry points, both converging on Core.Create:
//
//	// Direct (atomic with the business write via store.WithTx, or best-effort):
//	_, err := core.Create(ctx, auditlog.NewAuditLog{
//	    ModelType: "widget", ModelID: id, CreatedBy: actor, Payload: snapshot,
//	})
//
//	// Decoupled, over the in-process eventbus (producer doesn't import auditlog):
//	auditbus.Register(bus, core)                 // once at startup
//	auditbus.Publish(ctx, bus, auditbus.Event{ModelType: "widget", ModelID: id, ...})
//
// # Recorders
//
// Transports/producers depend on the Recorder interface (best-effort Record), so
// recording can be synchronous or asynchronous without changing call sites:
//
//   - *Core              — synchronous; the only option that can be atomic (WithTx).
//   - AsyncRecorder      — in-process workers, sharded by model (per-model order),
//     non-blocking; lost on crash.
//   - auditqueue.Recorder — durable queue + Worker; survives crashes, smooths spikes.
//
// # Querying
//
//	last, err := core.QueryLastByModelID(ctx, "widget", id)
//	hist, err := core.QueryHistoryByModelID(ctx, "widget", id)
//	diff, err := core.QueryDiffByModelID(ctx, "widget", id, filter) // between two versions
//	recs, err := core.QueryDiffAllVersionByModelID(ctx, "widget", id, filter)
//
// The auditrest subpackage exposes a ready-made read-side handler group
// (history / diff / changes) mountable on any skit router in one call:
//
//	auditrest.NewHandlers(core).Routes(r.HandleApp)
//
// # Compaction
//
// Compact thins a model's history while always keeping the first and last
// versions. Two API entry points; scheduling is left to the caller's worker.
//
//   - Opportunistic, inline on write: WithAutoCompact makes Create compact the
//     model it just wrote, once every N versions (best-effort, gated so most
//     writes pay nothing) — often enough to keep history bounded on its own.
//
//   - On demand, in portions: CompactBatch compacts up to Limit models over a
//     version threshold. Call it from an API handler, a CLI command, or a worker;
//     loop until Result.Models is 0 to drain a backlog gently.
//
//     core := auditlog.NewCore(log, store, auditlog.WithAutoCompact(100, opts)) // inline
//     res, err := core.CompactBatch(ctx, auditlog.CompactBatchOptions{Threshold: 100, Limit: 50, Compact: opts})
//
// Scheduled compaction is wired with the worker package — auditlog ships no
// built-in loop:
//
//	tick := func(ctx context.Context) error {
//	    _, err := core.CompactBatch(ctx, auditlog.CompactBatchOptions{Threshold: 100, Limit: 50, Compact: opts})
//	    return err
//	}
//	group.Add(worker.NewLoop(log.Slog(), worker.LoopConfig{Name: "auditlog-compaction", Interval: time.Hour}, tick))
//
// # Concurrency
//
// Create is a read-modify-write (read latest version, insert version+1). The db
// schema declares UNIQUE (model_type, model_id, version): a racing insert at the
// same version fails with a duplicate-key error, and Create re-reads and retries
// (bounded) so concurrent distinct changes are not lost.
package auditlog
