package outbox

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
)

// Marshaler serializes a domain event value into the bytes stored as an event's
// payload. The default is encoding/json.Marshal; override per type with
// WithMarshaler (e.g. for protobuf).
type Marshaler func(v any) ([]byte, error)

// route is the transport descriptor resolved for one registered event type.
type route struct {
	eventType   string
	topic       string
	key         string
	contentType string
	marshal     Marshaler
}

// Registry maps a domain event's Go type to its transport route (event type
// name, topic, key, content type, and how to marshal it). It lets the domain
// publish a plain typed value while the routing — which broker topic, which key
// — stays a wiring concern: the domain never names a topic or exchange.
//
// Populate it once at startup with Register, then hand it to WithinTran / Bind.
// Registry is safe for concurrent reads after setup.
type Registry struct {
	mu     sync.RWMutex
	routes map[reflect.Type]route
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{routes: make(map[reflect.Type]route)}
}

// RouteOption customizes a registration.
type RouteOption func(*route)

// WithKey sets the routing/ordering key for the event type (RabbitMQ routing
// key, Kafka partition key, ...). Optional; defaults to empty.
func WithKey(key string) RouteOption {
	return func(r *route) { r.key = key }
}

// WithContentType overrides the payload MIME type (default "application/json").
func WithContentType(ct string) RouteOption {
	return func(r *route) { r.contentType = ct }
}

// WithMarshaler overrides how the event value is serialized (default JSON).
func WithMarshaler(m Marshaler) RouteOption {
	return func(r *route) {
		if m != nil {
			r.marshal = m
		}
	}
}

// Register maps the Go type T to a CloudEvents type name and a transport topic.
// A pointer type registers its element type, so emitting either T or *T
// resolves the same route.
//
// It panics on a wiring mistake — empty name/topic or a duplicate type — since
// these are caught at startup, not at runtime.
func Register[T any](r *Registry, eventType, topic string, opts ...RouteOption) {
	if eventType == "" {
		panic("outbox: Register: event type name is required")
	}
	if topic == "" {
		panic("outbox: Register: topic is required")
	}

	rt := route{
		eventType:   eventType,
		topic:       topic,
		contentType: "application/json",
		marshal:     json.Marshal,
	}
	for _, opt := range opts {
		opt(&rt)
	}

	key := normalize(reflect.TypeFor[T]())

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.routes[key]; dup {
		panic(fmt.Sprintf("outbox: Register: type %s already registered", key))
	}
	r.routes[key] = rt
}

// lookup resolves the route for a value's type. The value may be a pointer to a
// registered type.
func (r *Registry) lookup(v any) (route, error) {
	if v == nil {
		return route{}, fmt.Errorf("outbox: publish: event is nil")
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
