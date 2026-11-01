package queue

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ErrUnknownKind is the error of a task whose Kind has no handler in a Mux
// without a fallback. It comes marked Permanent: the task goes to the dead
// letters instead of being retried forever.
var ErrUnknownKind = errors.New("queue: no handler registered for kind")

// Mux routes the tasks of a queue to handlers by Kind, so one queue and its
// workers serve many kinds: register a JobFunc per Kind and pass Mux.Handle to
// Workers. The claim does not filter by Kind, so the Mux is what makes the
// Kind column route work.
//
// A Mux is safe for concurrent use. Register every kind at startup, before
// the workers run.
type Mux struct {
	mu       sync.RWMutex
	routes   map[string]JobFunc
	fallback JobFunc
}

// MuxOption customizes a Mux.
type MuxOption func(*Mux)

// WithFallback sets the handler invoked for a Kind with no registered handler.
// Without it, or with a nil fn, an unknown Kind fails with ErrUnknownKind.
func WithFallback(fn JobFunc) MuxOption {
	return func(m *Mux) {
		if fn != nil {
			m.fallback = fn
		}
	}
}

// NewMux builds an empty Mux. By default an unknown Kind fails with
// ErrUnknownKind; override that with WithFallback.
func NewMux(opts ...MuxOption) *Mux {
	m := &Mux{routes: make(map[string]JobFunc), fallback: unknownKind}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Register binds fn to kind. It returns an error — never panics — on an empty
// kind, a nil handler, or a duplicate kind, so wiring mistakes surface at
// startup where the caller can handle them.
func (m *Mux) Register(kind string, fn JobFunc) error {
	if kind == "" {
		return errors.New("queue: register: kind is required")
	}
	if fn == nil {
		return errors.New("queue: register: handler is nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, dup := m.routes[kind]; dup {
		return fmt.Errorf("queue: register: kind %q already registered", kind)
	}
	m.routes[kind] = fn
	return nil
}

// Handle is the JobFunc of the Mux: it dispatches t to the handler registered
// for t.Kind, or the fallback.
func (m *Mux) Handle(ctx context.Context, t Task) error {
	return m.lookup(t.Kind)(ctx, t)
}

// lookup returns the handler of kind, or the fallback.
func (m *Mux) lookup(kind string) JobFunc {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if fn, ok := m.routes[kind]; ok {
		return fn
	}
	return m.fallback
}

// has reports whether kind has a handler of its own, the fallback aside.
func (m *Mux) has(kind string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.routes[kind]
	return ok
}

// unknownKind fails a task whose kind has no handler, for good.
func unknownKind(_ context.Context, t Task) error {
	return Permanent(fmt.Errorf("%w: %q", ErrUnknownKind, t.Kind))
}
