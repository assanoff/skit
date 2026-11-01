package outbox

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
)

// route is the transport descriptor resolved for one registered event type.
type route struct {
	eventType string
	topic     string
}

// Registry maps a domain event's Go type to its event type name and topic, and
// encodes every event as JSON. It lets the domain publish a plain typed value
// while the routing stays a wiring concern: the domain never names a topic or
// exchange.
//
// Populate it once at startup with Register, then hand it to Bind.
// Registry is safe for concurrent reads after setup.
type Registry struct {
	mu     sync.RWMutex
	routes map[reflect.Type]route
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{routes: make(map[reflect.Type]route)}
}

// Register maps the Go type T to a CloudEvents type name and a transport topic.
// A pointer type registers its element type, so emitting either T or *T
// resolves the same route.
//
// It returns an error — it never panics — on a wiring mistake (empty name/topic
// or a duplicate type), so the caller surfaces it at startup where the
// registration is wired.
func (r *Registry) Register[T any](eventType, topic string) error {
	if eventType == "" {
		return errors.New("outbox: register: event type name is required")
	}
	if topic == "" {
		return errors.New("outbox: register: topic is required")
	}

	rt := route{eventType: eventType, topic: topic}
	key := normalize(reflect.TypeFor[T]())

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.routes[key]; dup {
		return fmt.Errorf("outbox: register: type %s already registered", key)
	}
	r.routes[key] = rt
	return nil
}

// lookup resolves the route for a value's type. The value may be a pointer to a
// registered type.
func (r *Registry) lookup(v any) (route, error) {
	if v == nil {
		return route{}, errors.New("outbox: publish: event is nil")
	}
	key := normalize(reflect.TypeOf(v))

	r.mu.RLock()
	rt, ok := r.routes[key]
	r.mu.RUnlock()
	if !ok {
		return route{}, fmt.Errorf("outbox: publish: type %s is not registered", key)
	}
	return rt, nil
}

// normalize collapses a pointer type to its element so registration and lookup
// agree whether the caller uses T or *T.
func normalize(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}
