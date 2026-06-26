package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/assanoff/skit/errs"
)

// Source leases a batch of pending items for processing. Implementations must be
// safe to run from multiple replicas concurrently — the canonical SQL backing is
// SELECT ... FOR UPDATE SKIP LOCKED, which hands each item to exactly one caller.
// now is the processor's wall clock for the tick (passed in so it is testable);
// limit bounds the batch size.
type Source[T any] interface {
	Claim(ctx context.Context, now time.Time, limit int) ([]T, error)
}

// Handler processes one claimed item. A nil error means success (the item is
// passed to Sink.MarkDone); a non-nil error routes the item to Sink.MarkFailed,
// with terminality decided by the processor's IsTerminal classifier.
type Handler[T any] interface {
	Handle(ctx context.Context, item T) error
}

// Sink records the outcome of processing an item. MarkDone acknowledges success;
// MarkFailed records a failure, where terminal distinguishes a permanent failure
// (move to a dead state) from a retryable one (return to pending for a later
// tick). errMsg has already been passed through errs.Sanitize by the processor.
type Sink[T any] interface {
	MarkDone(ctx context.Context, item T, now time.Time) error
	MarkFailed(ctx context.Context, item T, errMsg string, terminal bool, now time.Time) error
}

// SourceFunc adapts a plain function to a Source.
type SourceFunc[T any] func(ctx context.Context, now time.Time, limit int) ([]T, error)

// Claim implements Source.
func (f SourceFunc[T]) Claim(ctx context.Context, now time.Time, limit int) ([]T, error) {
	return f(ctx, now, limit)
}

// HandlerFunc adapts a plain function to a Handler.
type HandlerFunc[T any] func(ctx context.Context, item T) error

// Handle implements Handler.
func (f HandlerFunc[T]) Handle(ctx context.Context, item T) error { return f(ctx, item) }

// ProcessorConfig configures a Processor.
type ProcessorConfig struct {
	// Name labels the processor in logs (defaults to "processor").
	Name string
	// BatchSize is the max number of items leased per tick (defaults to 100).
	BatchSize int
	// HandleTimeout bounds each Handle call and its follow-up mark (0 = inherit
	// the tick context with no extra bound).
	HandleTimeout time.Duration
	// IsTerminal classifies a Handle error as permanent (true) or retryable
	// (false). When nil, every failure is treated as retryable.
	IsTerminal func(err error) bool
	// Now returns the tick clock; defaults to time.Now().UTC. Override in tests.
	Now func() time.Time
}

func (c ProcessorConfig) withDefaults() ProcessorConfig {
	if c.Name == "" {
		c.Name = "processor"
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 100
	}
	if c.IsTerminal == nil {
		c.IsTerminal = func(error) bool { return false }
	}
	if c.Now == nil {
		c.Now = func() time.Time { return time.Now().UTC() }
	}
	return c
}

// Processor is the reliable batch-processing FSM shared by the outbox relay,
// push pipelines, and pgqueue consumers: each tick leases a batch from the
// Source, runs the Handler on each item, then records the outcome via the Sink
// (done on success, failed/terminal-or-retryable on error).
//
// A Processor is not itself a Runnable; call Tick to get a TickFunc and wrap it
// in a Loop to run it on a schedule. Run several processors over the same Source
// for higher throughput — SKIP LOCKED coordinates the claims.
type Processor[T any] struct {
	src  Source[T]
	h    Handler[T]
	sink Sink[T]
	cfg  ProcessorConfig
	log  *slog.Logger
}

// NewProcessor builds a Processor. log may be nil.
func NewProcessor[T any](log *slog.Logger, src Source[T], h Handler[T], sink Sink[T], cfg ProcessorConfig) *Processor[T] {
	return &Processor[T]{src: src, h: h, sink: sink, cfg: cfg.withDefaults(), log: log}
}

// Tick returns the processor's drain cycle as a TickFunc suitable for NewLoop
// (fixed interval).
func (p *Processor[T]) Tick() TickFunc { return p.process }

// PacedTick returns the drain cycle as a PacedTickFunc for NewPacedLoop: it
// reports Busy when it leased a full batch (a backlog likely remains, so the
// adaptive loop drains again promptly) and Idle otherwise.
func (p *Processor[T]) PacedTick() PacedTickFunc {
	return func(ctx context.Context) (Pace, error) {
		n, err := p.processN(ctx)
		if err != nil {
			return Idle, err
		}
		if n >= p.cfg.BatchSize {
			return Busy, nil
		}
		return Idle, nil
	}
}

// process runs one full claim -> handle -> mark cycle. It returns an error only
// when the claim itself fails (so the surrounding Loop can log it); per-item
// failures are recorded via the Sink and do not abort the batch.
func (p *Processor[T]) process(ctx context.Context) error {
	_, err := p.processN(ctx)
	return err
}

// processN is process plus the number of items claimed this tick (used by
// PacedTick to detect a full batch).
func (p *Processor[T]) processN(ctx context.Context) (int, error) {
	now := p.cfg.Now()

	items, err := p.src.Claim(ctx, now, p.cfg.BatchSize)
	if err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return 0, nil
	}

	for _, item := range items {
		p.handleOne(ctx, item)
	}
	return len(items), nil
}

// handleOne processes a single item and records its outcome. Mark failures are
// logged but not propagated — the surrounding tick has no caller to surface them
// to, and the next tick (or the sweeper) will reclaim an unacknowledged item.
func (p *Processor[T]) handleOne(ctx context.Context, item T) {
	itemCtx := ctx
	if p.cfg.HandleTimeout > 0 {
		var cancel context.CancelFunc
		itemCtx, cancel = context.WithTimeout(ctx, p.cfg.HandleTimeout)
		defer cancel()
	}

	now := p.cfg.Now()
	handleErr := p.h.Handle(itemCtx, item)

	var markErr error
	if handleErr != nil {
		terminal := p.cfg.IsTerminal(handleErr)
		markErr = p.sink.MarkFailed(itemCtx, item, errs.Sanitize(handleErr.Error()), terminal, now)
	} else {
		markErr = p.sink.MarkDone(itemCtx, item, now)
	}

	if markErr != nil && p.log != nil {
		p.log.Error(
			"processor mark failed",
			"worker", p.cfg.Name,
			"handle_err", handleErr,
			"mark_err", markErr,
		)
	}
}
