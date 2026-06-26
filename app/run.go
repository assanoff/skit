package app

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/assanoff/skit/closer"
	"github.com/assanoff/skit/worker"
)

// defaultSignals are the OS signals that trigger graceful shutdown.
var defaultSignals = []os.Signal{syscall.SIGINT, syscall.SIGTERM}

// RunConfig configures the long-lived run loop.
type RunConfig struct {
	// Logger is passed to the worker.Group for supervision logging (may be nil).
	Logger *slog.Logger
	// ShutdownTimeout bounds how long the group waits for runnables to Stop
	// (0 means no bound).
	ShutdownTimeout time.Duration
	// Signals override the shutdown signals (default SIGINT, SIGTERM).
	Signals []os.Signal
}

// Run supervises the given runnables until a shutdown signal arrives or one of
// them fails, then performs a graceful shutdown and releases every resource
// registered with the global closer (LIFO). It owns the signal context and the
// closer lifecycle, so a serve command shrinks to: build deps, build the server
// set, call Run.
//
// It returns nil on a clean, signal-driven shutdown and the triggering error
// when a runnable fails on its own.
func Run(ctx context.Context, cfg RunConfig, runnables ...worker.Runnable) error {
	sigs := cfg.Signals
	if len(sigs) == 0 {
		sigs = defaultSignals
	}

	sigCtx, stop := signal.NotifyContext(ctx, sigs...)
	defer stop()
	// Release resources (DB, tracer, broker, ...) in LIFO order, even on a
	// supervisor error during startup.
	defer func() { _ = closer.CloseSync() }()

	g := worker.NewGroup(cfg.Logger, cfg.ShutdownTimeout)
	g.Add(runnables...)

	if err := g.Run(sigCtx); err != nil && sigCtx.Err() == nil {
		return err
	}
	return nil
}

// CommandConfig configures RunCommand.
type CommandConfig struct {
	// Signals override the cancellation signals (default SIGINT, SIGTERM); the
	// context passed to fn is canceled when one arrives, so a long command can
	// abort cleanly.
	Signals []os.Signal
}

// RunCommand bootstraps a one-shot command: it initializes the given (possibly
// partial) set of dependencies on d — registering their cleanups with the global
// closer — runs fn with a signal-cancelable context, then releases every
// resource via the closer (LIFO), even when fn fails.
//
// This is the CLI counterpart of Run: a command (e.g. connect to the DB, run a
// business function, publish to a queue) assembles only the dependencies it
// needs by passing a subset of the application's initializers (compose named
// groups with slices.Concat), or the full set — dim is lazy, so only the
// dependencies fn actually touches are built.
func RunCommand[D any](ctx context.Context, cfg CommandConfig, d *D, inits []Initializer[D], fn func(ctx context.Context, d *D) error) error {
	sigs := cfg.Signals
	if len(sigs) == 0 {
		sigs = defaultSignals
	}

	sigCtx, stop := signal.NotifyContext(ctx, sigs...)
	defer stop()
	defer func() { _ = closer.CloseSync() }()

	if err := InitDeps(d, inits); err != nil {
		return err
	}
	return fn(sigCtx, d)
}
