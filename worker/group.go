package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// Group supervises a set of Runnables. Run starts them all concurrently and
// returns when ctx is canceled or any Runnable returns a non-nil, non-canceled
// error — whichever comes first — after which every Runnable is asked to Stop.
//
// This replaces hand-rolled run loops that juggle separate slices of servers,
// consumers, and workers: register everything as a Runnable and supervise it
// here.
type Group struct {
	log             *slog.Logger
	runnables       []Runnable
	shutdownTimeout time.Duration
}

// NewGroup builds a Group. shutdownTimeout bounds how long Run waits for all
// Runnables to Stop (0 means no bound). log may be nil.
func NewGroup(log *slog.Logger, shutdownTimeout time.Duration) *Group {
	return &Group{log: log, shutdownTimeout: shutdownTimeout}
}

// Add registers one or more Runnables, skipping any nil entries — so addr-gated
// constructors that return nil for a disabled brick (e.g. server.REST,
// server.Enabled) can be passed straight through without the caller filtering
// them first. Not safe to call once Run has started.
func (g *Group) Add(r ...Runnable) {
	for _, x := range r {
		if x != nil {
			g.runnables = append(g.runnables, x)
		}
	}
}

// Run starts all Runnables and blocks until shutdown. It always attempts to Stop
// every Runnable before returning, and returns the first triggering error (nil
// on a clean, ctx-driven shutdown).
func (g *Group) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, len(g.runnables))
	var wg sync.WaitGroup

	for _, r := range g.runnables {
		wg.Add(1)
		go func(r Runnable) {
			defer wg.Done()
			g.logf("worker: starting", "name", r.Name())
			if err := r.Start(runCtx); err != nil && !errors.Is(err, context.Canceled) {
				g.logf("worker: stopped with error", "name", r.Name(), "err", err)
				errCh <- err
				cancel() // one failure tears down the group
			}
		}(r)
	}

	// Wait for either external cancellation or the first internal failure.
	var trigger error
	select {
	case <-ctx.Done():
	case trigger = <-errCh:
	}
	cancel()

	g.stopAll()

	wg.Wait()
	return trigger
}

func (g *Group) stopAll() {
	stopCtx := context.Background()
	var cancel context.CancelFunc
	if g.shutdownTimeout > 0 {
		stopCtx, cancel = context.WithTimeout(stopCtx, g.shutdownTimeout)
		defer cancel()
	}

	// Stop in reverse registration order (LIFO), mirroring closer semantics.
	for i := len(g.runnables) - 1; i >= 0; i-- {
		r := g.runnables[i]
		if err := r.Stop(stopCtx); err != nil {
			g.logf("worker: stop failed", "name", r.Name(), "err", err)
		}
	}
}

func (g *Group) logf(msg string, args ...any) {
	if g.log != nil {
		g.log.Info(msg, args...)
	}
}
