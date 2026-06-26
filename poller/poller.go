package poller

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/assanoff/skit/safetick"
)

// Getter fetches the next value. It is called on each tick (and synchronously by
// Poll). A returned error leaves the current value unchanged.
type Getter[T any] func(ctx context.Context) (T, error)

// Config configures a Poller.
type Config struct {
	// Name identifies the poller in logs and panic metrics (defaults to "poller").
	Name string
	// Interval between polls. Required (> 0).
	Interval time.Duration
	// PollTimeout bounds each Getter call (0 = inherit the run context).
	PollTimeout time.Duration
	// OnError is invoked with each failed poll's error; may be nil (the error is
	// then only logged).
	OnError func(error)
	// OnPanic is invoked when a poll panics; may be nil.
	OnPanic safetick.PanicHandler
}

// Poller caches the latest value returned by its Getter, refreshing it on a
// fixed interval. Reads via Current are safe for concurrent use.
type Poller[T any] struct {
	cfg    Config
	getter Getter[T]
	log    *slog.Logger

	mu      sync.RWMutex
	current T
}

// New builds a Poller seeded with initial, which Current returns until the first
// successful poll. log may be nil.
func New[T any](log *slog.Logger, initial T, getter Getter[T], cfg Config) *Poller[T] {
	if cfg.Name == "" {
		cfg.Name = "poller"
	}
	return &Poller[T]{cfg: cfg, getter: getter, log: log, current: initial}
}

// Name identifies the poller in the supervisor and logs.
func (p *Poller[T]) Name() string { return p.cfg.Name }

// Start polls immediately, then on every Interval, until ctx is canceled. It
// implements worker.Runnable: Start blocks until shutdown.
func (p *Poller[T]) Start(ctx context.Context) error {
	ticker := time.NewTicker(p.cfg.Interval)
	defer ticker.Stop()

	p.Poll(ctx) // seed with a fresh value before the first interval elapses
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			p.Poll(ctx)
		}
	}
}

// Stop is a no-op: Start exits on ctx cancellation and the poller owns no
// external resources.
func (p *Poller[T]) Stop(context.Context) error { return nil }

// Poll fetches once, synchronously, and updates the current value on success.
// On error the value is left unchanged and OnError (if set) is invoked. A panic
// in the Getter is recovered so a single bad poll cannot crash the supervisor.
func (p *Poller[T]) Poll(ctx context.Context) {
	defer safetick.RecoverTick(p.log, p.cfg.Name, p.cfg.OnPanic)

	pollCtx := ctx
	if p.cfg.PollTimeout > 0 {
		var cancel context.CancelFunc
		pollCtx, cancel = context.WithTimeout(ctx, p.cfg.PollTimeout)
		defer cancel()
	}

	next, err := p.getter(pollCtx)
	if err != nil {
		if p.cfg.OnError != nil {
			p.cfg.OnError(err)
		}
		if p.log != nil {
			p.log.Error("poller poll failed", "poller", p.cfg.Name, "err", err)
		}
		return
	}

	p.mu.Lock()
	p.current = next
	p.mu.Unlock()
}

// Current returns the most recently fetched value (the New initial value until
// the first successful poll). Safe for concurrent use.
func (p *Poller[T]) Current() T {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.current
}
