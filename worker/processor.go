package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/otel"
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

// Releaser returns claimed items to their source untouched: no attempt is
// counted and each item keeps its place. A Processor with FailFast releases the
// rest of a batch it stops through its Sink, when the Sink implements Releaser;
// otherwise the rest stays claimed until the source reclaims it on its own,
// such as by a lease that expires. items is never empty.
type Releaser[T any] interface {
	Release(ctx context.Context, items []T) error
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
func (f HandlerFunc[T]) Handle(ctx context.Context, item T) error {
	return f(ctx, item)
}

// ProcessorConfig configures a Processor.
type ProcessorConfig struct {
	// Name labels the processor in logs (defaults to "processor").
	Name string
	// BatchSize is the max number of items leased per tick (defaults to 100).
	BatchSize int
	// HandleTimeout bounds each Handle call and, independently, its follow-up
	// mark: each gets its own budget so a slow-but-successful Handle cannot
	// starve the mark (which would leave the item unacknowledged and cause a
	// redelivery). 0 = inherit the tick context with no extra bound.
	HandleTimeout time.Duration
	// IsTerminal classifies a Handle error as permanent (true) or retryable
	// (false). When nil, every failure is treated as retryable.
	IsTerminal func(err error) bool
	// FailFast stops a batch at its first retryable failure: that item is
	// marked failed, and the rest goes back to the source untouched through
	// the Sink as a Releaser. Set it when a retryable failure means a
	// dependency is down, so the rest of the batch would fail the same way; a
	// terminal failure concerns its item only and does not stop the batch.
	FailFast bool
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
// reports Busy when it handled a full batch (a backlog likely remains, so the
// adaptive loop drains again promptly) and Idle otherwise. A batch stopped by
// FailFast is Idle: a dependency that is down is retried at the idle pace.
func (p *Processor[T]) PacedTick() PacedTickFunc { return p.drain }

// process runs one full claim -> handle -> mark cycle. It returns an error only
// when the claim itself fails (so the surrounding Loop can log it); per-item
// failures are recorded via the Sink and abort the batch only with FailFast.
func (p *Processor[T]) process(ctx context.Context) error {
	_, err := p.drain(ctx)
	return err
}

// drain is process plus the pace of the tick.
func (p *Processor[T]) drain(ctx context.Context) (Pace, error) {
	now := p.cfg.Now()

	items, err := p.src.Claim(ctx, now, p.cfg.BatchSize)
	if err != nil {
		return Idle, err
	}

	for i, item := range items {
		if p.stops(p.handleOne(ctx, item)) {
			p.release(ctx, items[i+1:])
			return Idle, nil
		}
	}
	if len(items) >= p.cfg.BatchSize {
		return Busy, nil
	}
	return Idle, nil
}

// stops reports whether err of Handle stops the batch: with FailFast, a
// retryable failure does.
func (p *Processor[T]) stops(err error) bool {
	return err != nil && p.cfg.FailFast && !p.cfg.IsTerminal(err)
}

// release hands the rest of a stopped batch back to the source, when the Sink
// is a Releaser. A failed release is logged: the rest stays claimed until the
// source reclaims it.
func (p *Processor[T]) release(ctx context.Context, rest []T) {
	r, ok := p.sink.(Releaser[T])
	if !ok || len(rest) == 0 {
		return
	}
	err := p.bounded(ctx, func(c context.Context) error {
		return r.Release(c, rest)
	})
	if err != nil && p.log != nil {
		p.log.ErrorContext(ctx, "processor release failed", "worker", p.cfg.Name, "err", err)
	}
}

// handleOne processes a single item, records its outcome and returns the error
// of Handle. Mark failures are logged but not propagated — the surrounding tick
// has no caller to surface them to, and the next tick (or the sweeper) will
// reclaim an unacknowledged item.
func (p *Processor[T]) handleOne(ctx context.Context, item T) error {
	now := p.cfg.Now()

	// The tick is suppressed; the item is work worth tracing, so its handler
	// runs with the mark lifted and sets the trace of the item itself. The
	// mark below stays suppressed.
	handleErr := p.bounded(otel.Unsuppress(ctx), func(c context.Context) error {
		return p.h.Handle(c, item)
	})

	// The mark gets its own timeout derived from the parent ctx, not the handle
	// context, so a slow handle cannot leave the mark on a near-expired deadline.
	markErr := p.bounded(ctx, func(c context.Context) error {
		if handleErr != nil {
			terminal := p.cfg.IsTerminal(handleErr)
			return p.sink.MarkFailed(c, item, errs.Sanitize(handleErr.Error()), terminal, now)
		}
		return p.sink.MarkDone(c, item, now)
	})

	if markErr != nil && p.log != nil {
		p.log.ErrorContext(ctx,
			"processor mark failed",
			"worker", p.cfg.Name,
			"handle_err", handleErr,
			"mark_err", markErr,
		)
	}
	return handleErr
}

// bounded runs op under a fresh HandleTimeout derived from ctx (each call gets
// its own budget); with no HandleTimeout it runs op on ctx directly.
func (p *Processor[T]) bounded(ctx context.Context, op func(context.Context) error) error {
	if p.cfg.HandleTimeout <= 0 {
		return op(ctx)
	}
	opCtx, cancel := context.WithTimeout(ctx, p.cfg.HandleTimeout)
	defer cancel()
	return op(opCtx)
}
