package eventbus

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/assanoff/skit/logger"
)

// Bus routes events to the handlers registered for each (domain, action). It is
// safe for concurrent use: registration and dispatch are guarded by a mutex,
// and handlers are snapshotted before they run so a handler may itself register
// without deadlocking.
type Bus struct {
	log   *logger.Logger
	mu    sync.RWMutex
	funcs map[string]map[string][]Func
}

// New constructs an empty Bus. log may be nil to disable logging.
func New(log *logger.Logger) *Bus {
	return &Bus{
		log:   log,
		funcs: make(map[string]map[string][]Func),
	}
}

// Register adds fn as a handler for the given domain and action. Multiple
// handlers may be registered for the same pair; they run in registration order.
// Registration typically happens once at construction time.
func (b *Bus) Register(domain, action string, fn Func) {
	b.mu.Lock()
	defer b.mu.Unlock()

	actions, ok := b.funcs[domain]
	if !ok {
		actions = make(map[string][]Func)
		b.funcs[domain] = actions
	}
	actions[action] = append(actions[action], fn)
}

// handlers returns a snapshot of the handlers for an event so dispatch does not
// hold the lock while running them.
func (b *Bus) handlers(data Data) []Func {
	b.mu.RLock()
	defer b.mu.RUnlock()

	fns := b.funcs[data.Domain][data.Action]
	if len(fns) == 0 {
		return nil
	}
	return append([]Func(nil), fns...)
}

// Call dispatches data to its handlers synchronously, stopping at and returning
// the first error. A pair with no handlers is a no-op. This is the right choice
// when a side effect's failure must abort the producer's operation.
func (b *Bus) Call(ctx context.Context, data Data) error {
	fns := b.handlers(data)
	if b.log != nil {
		b.log.Debug(ctx, "eventbus.call", "domain", data.Domain, "action", data.Action, "handlers", len(fns))
	}

	for _, fn := range fns {
		if err := fn(ctx, data); err != nil {
			if b.log != nil {
				b.log.Error(ctx, "eventbus.call handler failed", "domain", data.Domain, "action", data.Action, "err", err)
			}
			return fmt.Errorf("eventbus: %s/%s: %w", data.Domain, data.Action, err)
		}
	}
	return nil
}

// Publish dispatches data to every handler regardless of individual failures
// and returns their errors joined with errors.Join (nil when all succeed). Use
// it for independent, best-effort notifications where one handler's failure
// should not prevent the others from running.
func (b *Bus) Publish(ctx context.Context, data Data) error {
	fns := b.handlers(data)
	if b.log != nil {
		b.log.Debug(ctx, "eventbus.publish", "domain", data.Domain, "action", data.Action, "handlers", len(fns))
	}

	var errs []error
	for _, fn := range fns {
		if err := fn(ctx, data); err != nil {
			if b.log != nil {
				b.log.Error(ctx, "eventbus.publish handler failed", "domain", data.Domain, "action", data.Action, "err", err)
			}
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("eventbus: %s/%s: %w", data.Domain, data.Action, errors.Join(errs...))
}
