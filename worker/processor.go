package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/otel"
	"github.com/assanoff/skit/safetick"
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
// counted and each item keeps its place. A Processor releases the rest of a
// batch it stops — at an error IsHalt accepts, at BatchTimeout or when its tick is
// canceled — through its Sink, when the Sink implements Releaser; otherwise the
// rest stays claimed until the source reclaims it on its own, such as by a
// lease that expires. items is never empty.
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
	// HandleTimeout bounds each Handle call. 0 = inherit the tick context with
	// no extra bound. A mark or a release is not bounded by it: each runs on
	// the tick context without its cancellation, under its own 5s timeout, so
	// the outcome of a handled item is recorded even when the Handle was slow
	// or the tick was canceled meanwhile.
	HandleTimeout time.Duration
	// BatchTimeout stops a batch that has run this long since its claim: no
	// further item starts, and the rest goes back to the source through the
	// Sink as a Releaser. Set it to the lease of the source minus HandleTimeout
	// and a margin, so that no item is handled past its lease. 0 = no bound.
	BatchTimeout time.Duration
	// IsTerminal classifies a Handle error as permanent (true) or retryable
	// (false). When nil, every failure is treated as retryable.
	IsTerminal func(err error) bool
	// IsHalt reports whether a Handle error stops the batch: that item is
	// marked failed as IsTerminal says, and the rest goes back to the source
	// untouched through the Sink as a Releaser. Have it accept the errors that
	// mean a dependency is down, so the rest of the batch would fail the same
	// way. When nil, no error stops the batch.
	IsHalt func(err error) bool
	// OnPanic decides the outcome of an item whose Handle panicked. The panic
	// is recovered and logged with its stack, then the item is marked with the
	// error OnPanic returns: nil marks it done, an error IsTerminal accepts
	// marks it terminal, any other reschedules it. item is the T of the
	// Processor. nil returns p: the item is retried like a retryable failure.
	OnPanic func(ctx context.Context, item any, p *safetick.PanicError) error
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
	if c.IsHalt == nil {
		c.IsHalt = func(error) bool { return false }
	}
	if c.OnPanic == nil {
		c.OnPanic = retryPanic
	}
	if c.Now == nil {
		c.Now = func() time.Time { return time.Now().UTC() }
	}
	return c
}

// retryPanic is the default OnPanic: the item is retried.
func retryPanic(_ context.Context, _ any, p *safetick.PanicError) error {
	return p
}

// markTimeout bounds each mark and each release of a Processor.
const markTimeout = 5 * time.Second

// Processor is the reliable batch-processing FSM shared by the outbox relay,
// push pipelines, and pgqueue consumers: each tick leases a batch from the
// Source, runs the Handler on each item, then records the outcome via the Sink
// (done on success, failed/terminal-or-retryable on error). A panic in the
// Handler is recovered per item and becomes its outcome through OnPanic.
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
// IsHalt is Idle: a dependency that is down is retried at the idle pace.
func (p *Processor[T]) PacedTick() PacedTickFunc { return p.drain }

// process runs one full claim -> handle -> mark cycle. It returns an error only
// when the claim itself fails (so the surrounding Loop can log it); per-item
// failures are recorded via the Sink and abort the batch only when IsHalt
// accepts them.
// A canceled tick or BatchTimeout stops the batch before its next item.
func (p *Processor[T]) process(ctx context.Context) error {
	_, err := p.drain(ctx)
	return err
}

// drain is process plus the pace of the tick.
func (p *Processor[T]) drain(ctx context.Context) (Pace, error) {
	start := p.cfg.Now()

	items, err := p.src.Claim(ctx, start, p.cfg.BatchSize)
	if err != nil {
		return Idle, err
	}

	for i, item := range items {
		if p.expired(ctx, start) {
			p.release(ctx, items[i:])
			break
		}
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

// expired reports whether the batch claimed at start must stop before its next
// item: the tick is canceled, or BatchTimeout has passed since the claim.
func (p *Processor[T]) expired(ctx context.Context, start time.Time) bool {
	if ctx.Err() != nil {
		return true
	}
	return p.cfg.BatchTimeout > 0 && p.cfg.Now().Sub(start) >= p.cfg.BatchTimeout
}

// stops reports whether err of Handle stops the batch.
func (p *Processor[T]) stops(err error) bool {
	return err != nil && p.cfg.IsHalt(err)
}

// release hands the rest of a stopped batch back to the source, when the Sink
// is a Releaser. A failed release is logged: the rest stays claimed until the
// source reclaims it.
func (p *Processor[T]) release(ctx context.Context, rest []T) {
	r, ok := p.sink.(Releaser[T])
	if !ok || len(rest) == 0 {
		return
	}
	err := settle(ctx, func(c context.Context) error {
		return r.Release(c, rest)
	})
	if err != nil && p.log != nil {
		p.log.ErrorContext(ctx, "processor release failed", "worker", p.cfg.Name, "err", err)
	}
}

// handleOne processes a single item, records its outcome and returns it: the
// error of Handle, or of OnPanic when Handle panicked. Mark failures are
// logged but not propagated — the surrounding tick has no caller to surface
// them to, and the next tick (or the sweeper) will reclaim an unacknowledged
// item.
func (p *Processor[T]) handleOne(ctx context.Context, item T) error {
	now := p.cfg.Now()

	handleErr := p.handle(ctx, item)

	// The mark is settled apart from the handle context, so a slow handle or
	// a canceled tick cannot leave the item unacknowledged.
	markErr := settle(ctx, func(c context.Context) error {
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

// handle runs Handle on item under HandleTimeout and returns the outcome of the
// item. A panic in Handle is recovered and logged, and OnPanic turns it into
// the outcome.
func (p *Processor[T]) handle(ctx context.Context, item T) error {
	// The tick is suppressed; the item is work worth tracing, so its handler
	// runs with the mark lifted and sets the trace of the item itself. The
	// mark stays suppressed.
	err := p.bounded(otel.Unsuppress(ctx), func(c context.Context) error {
		return safetick.Call(func() error { return p.h.Handle(c, item) })
	})
	pe, ok := errors.AsType[*safetick.PanicError](err)
	if !ok {
		return err
	}
	if p.log != nil {
		p.log.ErrorContext(ctx,
			"processor handler panic recovered",
			"worker", p.cfg.Name,
			"err", pe,
			string(semconv.ExceptionStacktraceKey), string(pe.Stack),
		)
	}
	return p.cfg.OnPanic(ctx, item, pe)
}

// settle runs op, a mark or a release, on ctx without its cancellation and
// under its own markTimeout: the outcome of a handled item is recorded even
// when the tick is canceled or past its deadline.
func settle(ctx context.Context, op func(context.Context) error) error {
	c, cancel := context.WithTimeout(context.WithoutCancel(ctx), markTimeout)
	defer cancel()
	return op(c)
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
