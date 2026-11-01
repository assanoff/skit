package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"
)

// Encoder writes a scheduled value into the Payload of its task. ctx is the
// context of Schedule, so an encoder may carry what it holds, such as the
// trace of the producer, into the payload.
type Encoder interface {
	Encode(ctx context.Context, v any) ([]byte, error)
}

// jsonEncoder is the default Encoder, encoding/json.
type jsonEncoder struct{}

func (jsonEncoder) Encode(_ context.Context, v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("json encoder: %w", err)
	}
	return b, nil
}

// route is where a Scheduler sends the values of one registered type.
type route struct {
	queue string
	kind  string
	enc   Encoder
	drop  bool
}

// RouteOption customizes the route of a registered type.
type RouteOption func(*route)

// WithEncoder sets how the values of the type are written into the Payload
// (default JSON). A nil e keeps the default.
func WithEncoder(e Encoder) RouteOption {
	return func(r *route) {
		if e != nil {
			r.enc = e
		}
	}
}

// Drop makes Schedule write nothing for the values of the type and return
// nil: an integration that is turned off, such as events with no broker. The
// domain write goes through, and the domain does not branch.
func Drop() RouteOption {
	return func(r *route) { r.drop = true }
}

// Registry maps the Go type of a scheduled value to the queue and kind of its
// task, so the domain schedules a plain typed value and the routing stays a
// wiring concern: the domain never names a queue.
//
// Populate it once at startup with Register, check it against the Mux of each
// queue with Check, then hand it to Bind. A Registry is safe for concurrent
// reads after setup.
type Registry struct {
	mu     sync.RWMutex
	routes map[reflect.Type]route
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{routes: make(map[reflect.Type]route)}
}

// Register routes the values of the Go type T to kind in queue, written into
// the Payload as JSON unless WithEncoder sets another Encoder. A pointer type
// registers its element type, so scheduling T or *T resolves the same route.
//
// It returns an error — it never panics — on an empty queue or kind, or a type
// registered twice, so the caller surfaces it at startup where the
// registration is wired.
func (r *Registry) Register[T any](queue, kind string, opts ...RouteOption) error {
	switch {
	case queue == "":
		return errors.New("queue: register: queue is required")
	case kind == "":
		return errors.New("queue: register: kind is required")
	}
	rt := route{queue: queue, kind: kind, enc: jsonEncoder{}}
	for _, opt := range opts {
		opt(&rt)
	}
	key := normalize(reflect.TypeFor[T]())

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.routes[key]; dup {
		return fmt.Errorf("queue: register: type %s already registered", key)
	}
	r.routes[key] = rt
	return nil
}

// Check reports the kinds the Registry sends to queue that have no handler of
// their own in m: their tasks would go to the dead letters as unknown. A
// fallback of m does not count, and a route with Drop sends nothing. Call it
// at startup for each queue with its Mux.
func (r *Registry) Check(queue string, m *Mux) error {
	if m == nil {
		return errors.New("queue: check: mux is nil")
	}
	var missing []string
	for _, kind := range r.kinds(queue) {
		if !m.has(kind) {
			missing = append(missing, kind)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("queue: check %s: %w: %q", queue, ErrUnknownKind, missing)
}

// kinds returns the sorted kinds the Registry sends to queue, the routes with
// Drop aside.
func (r *Registry) kinds(queue string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []string
	for _, rt := range r.routes {
		if rt.queue == queue && !rt.drop {
			out = append(out, rt.kind)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// lookup returns the route of the type of v, which may be a pointer to a
// registered type.
func (r *Registry) lookup(v any) (route, error) {
	if v == nil {
		return route{}, errors.New("queue schedule: value is nil")
	}
	key := normalize(reflect.TypeOf(v))

	r.mu.RLock()
	rt, ok := r.routes[key]
	r.mu.RUnlock()
	if !ok {
		return route{}, fmt.Errorf("queue schedule: type %s is not registered", key)
	}
	return rt, nil
}

// normalize collapses a pointer type to its element, so registration and
// lookup agree whether the caller uses T or *T.
func normalize(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}
