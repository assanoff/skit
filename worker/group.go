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

// NewGroup builds a Group. shutdownTimeout bounds the graceful-shutdown phase:
// it caps both each Runnable's Stop call and the subsequent wait for every Start
// goroutine to return, so a Runnable whose Start ignores ctx cannot hang Run
// forever (0 means no bound — wait indefinitely). log may be nil.
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

	if !g.waitForStop(&wg) {
		g.logf("worker: shutdown timed out waiting for Runnables to stop")
	}
	return trigger
}

// waitForStop waits for every Start goroutine to return, bounded by
// shutdownTimeout. It reports true if they all drained, or false if the timeout
// elapsed first. A non-positive shutdownTimeout waits indefinitely.
func (g *Group) waitForStop(wg *sync.WaitGroup) bool {
	if g.shutdownTimeout <= 0 {
		wg.Wait()
		return true
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	timer := time.NewTimer(g.shutdownTimeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
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
