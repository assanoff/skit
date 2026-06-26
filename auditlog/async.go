package auditlog

import (
	"context"
	"hash/fnv"
	"log/slog"
	"sync"

	"github.com/assanoff/skit/safetick"
)

// AsyncConfig configures an AsyncRecorder.
type AsyncConfig struct {
	// Workers is the number of background workers (and shards). Entries for the
	// same model always go to the same shard, so per-model order is preserved.
	// Defaults to 4.
	Workers int
	// Buffer is the capacity of each shard's queue. Defaults to 256.
	Buffer int
	// BlockOnFull makes Record block until there is room instead of dropping.
	// Default (false) drops the entry and calls OnDrop, so the request path is
	// never slowed by a saturated audit pipeline.
	BlockOnFull bool
	// OnDrop is invoked with an entry dropped because its shard was full (when
	// BlockOnFull is false). Use it for a metric/log; may be nil.
	OnDrop func(NewAuditLog)
}

// AsyncRecorder records audit entries on background workers so the request path
// is not slowed by the database write. It implements Recorder (non-blocking
// Record) and worker.Runnable (Start/Stop/Name): supervise it in a worker.Group.
//
// Entries are sharded by (ModelType, ModelID) onto a fixed worker, so writes for
// the same model stay ordered — important because Core.Create derives the next
// version from the latest stored one. On shutdown, Start drains the buffered
// entries before returning.
type AsyncRecorder struct {
	core   *Core
	cfg    AsyncConfig
	log    *slog.Logger
	shards []chan job
}

type job struct {
	ctx   context.Context
	entry NewAuditLog
}

// NewAsyncRecorder builds an AsyncRecorder over core. It does not write anything
// until Start is called; Record before Start (or after Stop) drops.
func NewAsyncRecorder(core *Core, cfg AsyncConfig) *AsyncRecorder {
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.Buffer <= 0 {
		cfg.Buffer = 256
	}
	shards := make([]chan job, cfg.Workers)
	for i := range shards {
		shards[i] = make(chan job, cfg.Buffer)
	}
	var log *slog.Logger
	if core != nil && core.log != nil {
		log = core.log.Slog()
	}
	return &AsyncRecorder{core: core, cfg: cfg, log: log, shards: shards}
}

// Name identifies the recorder in the supervisor and logs.
func (a *AsyncRecorder) Name() string { return "auditlog-async" }

// Record enqueues entry onto its model's shard and returns immediately. The
// request context is detached (WithoutCancel) so the background write is not
// canceled when the request ends, while trace/values are preserved for logging.
// When the shard is full it blocks or drops per AsyncConfig.
func (a *AsyncRecorder) Record(ctx context.Context, entry NewAuditLog) {
	j := job{ctx: context.WithoutCancel(ctx), entry: entry}
	shard := a.shards[a.shardFor(entry)]
	if a.cfg.BlockOnFull {
		shard <- j
		return
	}
	select {
	case shard <- j:
	default:
		if a.cfg.OnDrop != nil {
			a.cfg.OnDrop(entry)
		}
		if a.log != nil {
			a.log.Warn("auditlog: async buffer full, dropping entry",
				"model_type", entry.ModelType, "model_id", entry.ModelID)
		}
	}
}

func (a *AsyncRecorder) shardFor(entry NewAuditLog) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(entry.ModelType))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(entry.ModelID))
	return int(h.Sum32() % uint32(len(a.shards)))
}

// Start runs the workers until ctx is canceled, then drains the buffered entries
// and returns. It implements worker.Runnable.
func (a *AsyncRecorder) Start(ctx context.Context) error {
	var wg sync.WaitGroup
	wg.Add(len(a.shards))
	for i := range a.shards {
		go func(ch chan job) {
			defer wg.Done()
			a.drain(ctx, ch)
		}(a.shards[i])
	}
	wg.Wait()
	return nil
}

// drain processes a shard until ctx is canceled, then flushes whatever is
// already buffered so in-flight entries are not lost on graceful shutdown.
func (a *AsyncRecorder) drain(ctx context.Context, ch chan job) {
	for {
		select {
		case j := <-ch:
			a.write(j)
		case <-ctx.Done():
			for {
				select {
				case j := <-ch:
					a.write(j)
				default:
					return
				}
			}
		}
	}
}

func (a *AsyncRecorder) write(j job) {
	defer safetick.RecoverTick(a.log, "auditlog-async", nil)
	a.core.Record(j.ctx, j.entry)
}

// Stop is a no-op: Start exits and drains on ctx cancellation.
func (a *AsyncRecorder) Stop(context.Context) error { return nil }
