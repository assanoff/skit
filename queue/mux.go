package queue

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/assanoff/skit/worker"
)

// JobFunc handles one claimed task, selected by a Mux on Task.Kind. It takes the
// whole Task (not just Payload) so a handler can see Name/Attempts/Kind. A nil
// return acks the task (MarkDone); a non-nil error routes it to MarkFailed.
type JobFunc func(ctx context.Context, t Task) error

// ErrUnknownKind is returned by Mux.Handle for a task whose Kind has no
// registered handler. Classify it as terminal in your Processor (or set a
// fallback) so an unroutable task dead-letters instead of being retried forever.
var ErrUnknownKind = errors.New("queue: no handler registered for kind")

// Mux routes claimed tasks to handlers by Kind. It implements
// worker.Handler[Task], so one queue and a single worker pool can serve many
// task kinds: register a JobFunc per Kind and hand the Mux to a worker.Processor
// as its Handler. Claim does not filter by Kind, so the Mux is what makes the
// Kind column actually route work.
//
// A Mux is safe for concurrent use. Register every kind at startup, before the
// processor runs.
type Mux struct {
	mu       sync.RWMutex
	handlers map[string]JobFunc
	fallback JobFunc
}

// MuxOption customizes a Mux.
type MuxOption func(*Mux)

// WithFallback sets the handler invoked for a Kind with no registered handler.
// Without it, an unknown Kind yields ErrUnknownKind.
func WithFallback(fn JobFunc) MuxOption {
	return func(m *Mux) {
		m.fallback = fn
	}
}

// NewMux builds an empty Mux. By default an unknown Kind returns ErrUnknownKind;
// override that with WithFallback.
func NewMux(opts ...MuxOption) *Mux {
	m := &Mux{handlers: make(map[string]JobFunc)}
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
	if _, dup := m.handlers[kind]; dup {
		return fmt.Errorf("queue: register: kind %q already registered", kind)
	}
	m.handlers[kind] = fn
	return nil
}

// Handle implements worker.Handler[Task]: it dispatches t to the handler
// registered for t.Kind, or the fallback (default: ErrUnknownKind).
func (m *Mux) Handle(ctx context.Context, t Task) error {
	m.mu.RLock()
	fn, ok := m.handlers[t.Kind]
	fallback := m.fallback
	m.mu.RUnlock()

	if !ok {
		if fallback != nil {
			return fallback(ctx, t)
		}
		return fmt.Errorf("%w: %q", ErrUnknownKind, t.Kind)
	}
	return fn(ctx, t)
}

var _ worker.Handler[Task] = (*Mux)(nil)
