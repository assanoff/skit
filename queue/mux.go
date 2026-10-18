package queue

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

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
	routes   map[string]route
	fallback route
	metrics  *muxMetrics
}

// route is the handler of a kind and the metrics of its tasks.
type route struct {
	fn      JobFunc
	metrics kindMetrics
}

// MuxOption customizes a Mux.
type MuxOption func(*muxOptions)

// muxOptions collects the options of NewMux.
type muxOptions struct {
	fallback JobFunc
	reg      prometheus.Registerer
}

// WithFallback sets the handler invoked for a Kind with no registered handler.
// Without it, an unknown Kind yields ErrUnknownKind.
func WithFallback(fn JobFunc) MuxOption {
	return func(o *muxOptions) {
		o.fallback = fn
	}
}

// WithMetrics records the tasks the Mux handles on reg:
//
//	skit_queue_handled_total{kind,result}    — tasks by result: ok or error
//	skit_queue_handle_duration_seconds{kind} — duration of the handler
//
// kind is the kind given to Register; a task of another kind is recorded as
// "<fallback>" with WithFallback and as "<unknown>" without it. A handler that
// panics is not recorded here: the panic ends the tick, and the Loop counts it
// in skit_worker_ticks_total{result="panic"}. A nil reg turns the metrics off:
// the Mux records them, but nothing exposes them.
func WithMetrics(reg prometheus.Registerer) MuxOption {
	return func(o *muxOptions) {
		o.reg = reg
	}
}

// NewMux builds an empty Mux. By default an unknown Kind returns ErrUnknownKind;
// override that with WithFallback.
func NewMux(opts ...MuxOption) *Mux {
	var o muxOptions
	for _, opt := range opts {
		opt(&o)
	}
	metrics := newMuxMetrics(o.reg)
	return &Mux{
		routes:   make(map[string]route),
		fallback: fallbackRoute(metrics, o.fallback),
		metrics:  metrics,
	}
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
	m.routes[kind] = route{fn: fn, metrics: m.metrics.kind(kind)}
	return nil
}

// Handle implements worker.Handler[Task]: it dispatches t to the handler
// registered for t.Kind, or the fallback (default: ErrUnknownKind).
func (m *Mux) Handle(ctx context.Context, t Task) error {
	r := m.lookup(t.Kind)
	start := time.Now()
	err := r.fn(ctx, t)
	r.metrics.observe(ctx, time.Since(start), err)
	return err
}

// lookup returns the route of kind, or the fallback.
func (m *Mux) lookup(kind string) route {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if r, ok := m.routes[kind]; ok {
		return r
	}
	return m.fallback
}

// fallbackRoute routes the kinds without a handler to fn, or to ErrUnknownKind
// when fn is nil.
func fallbackRoute(metrics *muxMetrics, fn JobFunc) route {
	if fn == nil {
		return route{fn: unknownKind, metrics: metrics.kind(kindUnknown)}
	}
	return route{fn: fn, metrics: metrics.kind(kindFallback)}
}

// unknownKind fails a task whose kind has no handler.
func unknownKind(_ context.Context, t Task) error {
	return fmt.Errorf("%w: %q", ErrUnknownKind, t.Kind)
}

var _ worker.Handler[Task] = (*Mux)(nil)
