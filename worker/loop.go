package worker

import (
	"cmp"
	"context"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/assanoff/skit/internal/prom"
	"github.com/assanoff/skit/internal/runspan"
	"github.com/assanoff/skit/otel"
	"github.com/assanoff/skit/safetick"
)

// scope names the tracer of this package.
const scope = "github.com/assanoff/skit/worker"

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
	// Name identifies the loop in logs and metrics (default "worker"); give
	// each loop its own.
	Name string
	// Interval between ticks (defaults to 1s if non-positive). In an adaptive loop
	// (NewPacedLoop) this is the idle interval — the wait after a tick that found
	// no work.
	Interval time.Duration
	// ImmediateFirstTick runs a tick at startup before the first interval.
	ImmediateFirstTick bool
	// TickTimeout bounds each tick (0 = inherit the loop context with no extra bound).
	TickTimeout time.Duration
	// Metrics is the registry of the tick metrics (skit_worker_ticks_total,
	// skit_worker_tick_duration_seconds,
	// skit_worker_last_success_timestamp_seconds, by worker = Name); nil
	// disables them.
	Metrics prometheus.Registerer
	// Trace records each tick as the root span of its own trace, named Name.
	// false (the default) suppresses the tick instead (otel.Suppress): nothing
	// under it is traced but the items a Processor handles, which lift the mark.
	Trace bool
	// TracerProvider records the tick spans when Trace is set; nil → the global
	// provider.
	TracerProvider trace.TracerProvider

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
	cfg     LoopConfig
	tick    TickFunc
	paced   PacedTickFunc
	log     *slog.Logger
	onPanic safetick.PanicHandler
	runs    *prom.Runs
	tracer  trace.Tracer // nil unless the ticks are traced
}

// LoopOption customizes a Loop.
type LoopOption func(*Loop)

// WithOnPanic sets a handler invoked when a tick panics. The panic is always
// recovered, logged and counted regardless.
func WithOnPanic(h safetick.PanicHandler) LoopOption {
	return func(l *Loop) { l.onPanic = h }
}

// NewLoop builds a fixed-interval Loop. A non-positive Interval defaults to 1s
// (so a zero-value config never panics in time.NewTicker), matching NewPacedLoop.
// log may be nil.
func NewLoop(log *slog.Logger, cfg LoopConfig, tick TickFunc, opts ...LoopOption) *Loop {
	l := newLoop(log, cfg, opts)
	l.tick = tick
	return l
}

// NewPacedLoop builds an adaptive Loop: after each tick it waits BusyInterval
// (default: immediately) when the tick reported Busy, or Interval — growing
// toward MaxIdleInterval over consecutive idle ticks — when it reported Idle.
// This drains a backlog quickly yet idles cheaply. log may be nil.
func NewPacedLoop(log *slog.Logger, cfg LoopConfig, tick PacedTickFunc, opts ...LoopOption) *Loop {
	l := newLoop(log, cfg, opts)
	l.paced = tick
	return l
}

func newLoop(log *slog.Logger, cfg LoopConfig, opts []LoopOption) *Loop {
	cfg.Name = cmp.Or(cfg.Name, "worker")
	if cfg.Interval <= 0 {
		cfg.Interval = time.Second
	}
	ticks := prom.RunsOpts{Subsystem: "worker", Run: "tick", Label: "worker"}
	l := &Loop{cfg: cfg, log: log, runs: prom.NewRuns(cfg.Metrics, ticks, cfg.Name)}
	if cfg.Trace {
		l.tracer = otel.Tracer(cfg.TracerProvider, scope)
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
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
			var next time.Duration
			next, idleDelay = l.nextDelay(l.runPacedTick(ctx), idleDelay)
			timer.Reset(next)
		}
	}
}

// nextDelay returns the delay before the next tick and the idle delay to carry
// on. A busy tick drains again after BusyInterval (0 = immediately) and resets
// the idle backoff; an idle tick waits the idle delay and grows it.
func (l *Loop) nextDelay(pace Pace, idle time.Duration) (next, carry time.Duration) {
	if pace == Busy {
		return l.cfg.BusyInterval, l.cfg.Interval
	}
	return idle, l.growIdle(idle)
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
	l.run(ctx, l.tick)
}

// runPacedTick runs an adaptive tick and returns its pace; a tick that
// panicked is Idle.
func (l *Loop) runPacedTick(ctx context.Context) Pace {
	var pace Pace
	l.run(ctx, func(ctx context.Context) error {
		p, err := l.paced(ctx)
		pace = p
		return err
	})
	return pace
}

// run runs tick under panic recovery and TickTimeout, logs its error and
// records it in the tick metrics and, when the loop traces its ticks, in the
// span of the tick.
func (l *Loop) run(ctx context.Context, tick TickFunc) {
	start := time.Now()
	ctx, span := l.startTick(ctx)
	var err error
	// finished stays false when tick panics: RecoverTick, deferred last, runs
	// first and stops the panic, then the tick is recorded as a panic.
	finished := false
	defer func() {
		l.runs.Record(ctx, time.Since(start), finished, err)
		runspan.End(span, finished, err)
	}()
	defer safetick.RecoverTick(ctx, l.log, l.cfg.Name, l.onPanic)

	tickCtx, cancel := l.tickContext(ctx)
	if cancel != nil {
		defer cancel()
	}
	err = tick(tickCtx)
	finished = true
	if err != nil && l.log != nil {
		l.log.ErrorContext(ctx, "worker tick error", "worker", l.cfg.Name, "err", err)
	}
}

// startTick returns the context of a tick: suppressed, or under the root span
// of the tick when the loop traces its ticks.
func (l *Loop) startTick(ctx context.Context) (context.Context, trace.Span) {
	if l.tracer == nil {
		return otel.Suppress(ctx), noop.Span{}
	}
	return runspan.Start(ctx, l.tracer, l.cfg.Name)
}

func (l *Loop) tickContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if l.cfg.TickTimeout > 0 {
		return context.WithTimeout(ctx, l.cfg.TickTimeout)
	}
	return ctx, nil
}
