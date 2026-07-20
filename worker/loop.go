package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/assanoff/skit/safetick"
)

// TickFunc performs one unit of periodic work. Returning an error logs it but
// does not stop the loop; the loop stops only when its context is canceled.
type TickFunc func(ctx context.Context) error

// Pace is the signal an adaptive tick returns to tell the loop how soon to run
// again: drain a backlog fast while there is work, idle cheaply when there is
// none.
type Pace int

const (
	// Idle means the tick found no (or only partial) work; the loop waits the
	// idle interval before the next tick.
	Idle Pace = iota
	// Busy means the tick drained a full batch, so a backlog likely remains; the
	// loop runs again after the (short) busy interval.
	Busy
)

// PacedTickFunc is a tick that reports its Pace so an adaptive loop can vary the
// delay between runs. Like TickFunc, a returned error is logged but does not
// stop the loop.
type PacedTickFunc func(ctx context.Context) (Pace, error)

// LoopConfig configures a Loop.
type LoopConfig struct {
	// Name identifies the loop in logs and panic metrics.
	Name string
	// Interval between ticks (defaults to 1s if non-positive). In an adaptive loop
	// (NewPacedLoop) this is the idle interval — the wait after a tick that found
	// no work.
	Interval time.Duration
	// ImmediateFirstTick runs a tick at startup before the first interval.
	ImmediateFirstTick bool
	// TickTimeout bounds each tick (0 = inherit the loop context with no extra bound).
	TickTimeout time.Duration
	// OnPanic is invoked when a tick panics (e.g. to bump a metric); may be nil.
	OnPanic safetick.PanicHandler

	// BusyInterval is the wait after a Busy tick in an adaptive loop (0 = run
	// again immediately to drain the backlog). Ignored by a fixed loop.
	BusyInterval time.Duration
	// MaxIdleInterval, when greater than Interval, makes an adaptive loop back
	// off geometrically (×2 per consecutive idle tick) from Interval up to this
	// cap, so a long-idle loop polls less often. 0 disables backoff (the loop
	// always waits Interval when idle). Ignored by a fixed loop.
	MaxIdleInterval time.Duration
}

// Loop is a Runnable that calls a tick on an interval, each tick wrapped in
// panic recovery so a single bad tick cannot crash the process. Built with
// NewLoop it runs on a fixed interval; built with NewPacedLoop it paces
// adaptively from the tick's reported Pace. This is the common shape behind the
// outbox/queue background workers.
type Loop struct {
	cfg   LoopConfig
	tick  TickFunc
	paced PacedTickFunc
	log   *slog.Logger
}

// NewLoop builds a fixed-interval Loop. A non-positive Interval defaults to 1s
// (so a zero-value config never panics in time.NewTicker), matching NewPacedLoop.
// log may be nil.
func NewLoop(log *slog.Logger, cfg LoopConfig, tick TickFunc) *Loop {
	if cfg.Interval <= 0 {
		cfg.Interval = time.Second
	}
	return &Loop{cfg: cfg, tick: tick, log: log}
}

// NewPacedLoop builds an adaptive Loop: after each tick it waits BusyInterval
// (default: immediately) when the tick reported Busy, or Interval — growing
// toward MaxIdleInterval over consecutive idle ticks — when it reported Idle.
// This drains a backlog quickly yet idles cheaply. log may be nil.
func NewPacedLoop(log *slog.Logger, cfg LoopConfig, tick PacedTickFunc) *Loop {
	if cfg.Interval <= 0 {
		cfg.Interval = time.Second
	}
	return &Loop{cfg: cfg, paced: tick, log: log}
}

// Name implements Runnable.
func (l *Loop) Name() string { return l.cfg.Name }

// Start implements Runnable: it ticks until ctx is canceled.
func (l *Loop) Start(ctx context.Context) error {
	if l.paced != nil {
		return l.startAdaptive(ctx)
	}
	return l.startFixed(ctx)
}

func (l *Loop) startFixed(ctx context.Context) error {
	ticker := time.NewTicker(l.cfg.Interval)
	defer ticker.Stop()

	if l.cfg.ImmediateFirstTick {
		l.runTick(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			l.runTick(ctx)
		}
	}
}

func (l *Loop) startAdaptive(ctx context.Context) error {
	idleDelay := l.cfg.Interval
	first := l.cfg.Interval
	if l.cfg.ImmediateFirstTick {
		first = 0
	}
	timer := time.NewTimer(first)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			pace := l.runPacedTick(ctx)
			var next time.Duration
			if pace == Busy {
				next = l.cfg.BusyInterval  // 0 = drain again immediately
				idleDelay = l.cfg.Interval // reset idle backoff
			} else {
				next = idleDelay
				idleDelay = l.growIdle(idleDelay)
			}
			timer.Reset(next)
		}
	}
}

// growIdle advances the idle delay for the next consecutive idle tick, doubling
// up to MaxIdleInterval. With no cap set, the idle delay stays at Interval.
func (l *Loop) growIdle(cur time.Duration) time.Duration {
	if l.cfg.MaxIdleInterval <= l.cfg.Interval {
		return l.cfg.Interval
	}
	return min(cur*2, l.cfg.MaxIdleInterval)
}

// Stop implements Runnable; the loop stops when its context is canceled.
func (l *Loop) Stop(context.Context) error { return nil }

func (l *Loop) runTick(ctx context.Context) {
	defer safetick.RecoverTick(l.log, l.cfg.Name, l.cfg.OnPanic)

	tickCtx, cancel := l.tickContext(ctx)
	if cancel != nil {
		defer cancel()
	}

	if err := l.tick(tickCtx); err != nil && l.log != nil {
		l.log.Error("worker tick error", "worker", l.cfg.Name, "err", err)
	}
}

func (l *Loop) runPacedTick(ctx context.Context) (pace Pace) {
	defer safetick.RecoverTick(l.log, l.cfg.Name, l.cfg.OnPanic)

	tickCtx, cancel := l.tickContext(ctx)
	if cancel != nil {
		defer cancel()
	}

	p, err := l.paced(tickCtx)
	if err != nil && l.log != nil {
		l.log.Error("worker tick error", "worker", l.cfg.Name, "err", err)
	}
	return p
}

func (l *Loop) tickContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if l.cfg.TickTimeout > 0 {
		return context.WithTimeout(ctx, l.cfg.TickTimeout)
	}
	return ctx, nil
}
