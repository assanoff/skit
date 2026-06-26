package outbox

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// fakeStore is a minimal Store that records inserted events; only WithTx and
// Insert are exercised by the producer-side tests.
type fakeStore struct{ inserted []Event }

func (s *fakeStore) WithTx(sqlx.ExtContext) Store { return s }
func (s *fakeStore) Insert(_ context.Context, events ...Event) error {
	s.inserted = append(s.inserted, events...)
	return nil
}

func (s *fakeStore) LeasePending(context.Context, time.Time, int) ([]Event, error) { return nil, nil }
func (s *fakeStore) MarkSent(context.Context, Event, uuid.UUID, time.Time) error   { return nil }
func (s *fakeStore) MarkFailed(context.Context, Event, uuid.UUID, string, time.Time) error {
	return nil
}

func (s *fakeStore) SweepExpiredLeases(context.Context, time.Duration, time.Time, int) (int64, error) {
	return 0, nil
}

func (s *fakeStore) Cleanup(context.Context, time.Duration, time.Time) (int64, error) { return 0, nil }

type widgetCreated struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func TestRegistryPublishResolvesRoute(t *testing.T) {
	reg := NewRegistry()
	Register[widgetCreated](reg, "widget.created", "widgets", WithKey("created"))

	store := &fakeStore{}
	pub := Bind(nil, store, reg)

	if err := pub.Publish(context.Background(), widgetCreated{ID: "42", Name: "gadget"}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(store.inserted) != 1 {
		t.Fatalf("expected 1 inserted event, got %d", len(store.inserted))
	}
	ev := store.inserted[0]
	if ev.Type != "widget.created" || ev.Topic != "widgets" || ev.Key != "created" {
		t.Errorf("route mismatch: type=%q topic=%q key=%q", ev.Type, ev.Topic, ev.Key)
	}
	if ev.ContentType != "application/json" {
		t.Errorf("content type = %q, want application/json", ev.ContentType)
	}
	var got widgetCreated
	if err := json.Unmarshal(ev.Payload, &got); err != nil {
		t.Fatalf("payload not JSON: %v", err)
	}
	if got != (widgetCreated{ID: "42", Name: "gadget"}) {
		t.Errorf("payload mismatch: %+v", got)
	}
}

func TestPublishInjectsTraceContext(t *testing.T) {
	// The propagator is normally set by otel.InitTracing; set it directly here.
	otel.SetTextMapPropagator(propagation.TraceContext{})

	reg := NewRegistry()
	Register[widgetCreated](reg, "widget.created", "widgets")
	store := &fakeStore{}
	pub := Bind(nil, store, reg)

	// A context carrying a valid (sampled) span context, as a request handler
	// would have after the otel middleware ran.
	traceID, _ := trace.TraceIDFromHex("0123456789abcdef0123456789abcdef")
	spanID, _ := trace.SpanIDFromHex("0123456789abcdef")
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	if err := pub.Publish(ctx, widgetCreated{ID: "1"}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	var headers map[string]string
	if err := json.Unmarshal(store.inserted[0].Headers, &headers); err != nil {
		t.Fatalf("headers not JSON: %v", err)
	}
	tp, ok := headers["traceparent"]
	if !ok {
		t.Fatalf("event headers missing traceparent: %v", headers)
	}
	if !strings.Contains(tp, traceID.String()) {
		t.Errorf("traceparent %q does not carry trace id %s", tp, traceID)
	}
}

func TestPublishWithoutTraceLeavesHeadersEmpty(t *testing.T) {
	reg := NewRegistry()
	Register[widgetCreated](reg, "widget.created", "widgets")
	store := &fakeStore{}
	pub := Bind(nil, store, reg)

	if err := pub.Publish(context.Background(), widgetCreated{ID: "1"}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if h := string(store.inserted[0].Headers); h != "{}" {
		t.Errorf("headers = %q, want %q (no trace context to inject)", h, "{}")
	}
}

func TestPublishOptionsOverrideRoute(t *testing.T) {
	// One Go payload type registered with a default route; each publish picks a
	// different CloudEvents type at runtime via As — the promocode/events shape.
	reg := NewRegistry()
	Register[widgetCreated](reg, "promo.created", "push-gateway", WithKey("push-gateway.send"))

	store := &fakeStore{}
	pub := Bind(nil, store, reg)

	if err := pub.Publish(context.Background(), widgetCreated{ID: "1"},
		As("amount.promo.created")); err != nil {
		t.Fatalf("publish As: %v", err)
	}
	if err := pub.Publish(context.Background(), widgetCreated{ID: "2"},
		As("percentage.promo.created"), OnTopic("other"), WithRouteKey("k2")); err != nil {
		t.Fatalf("publish overrides: %v", err)
	}

	if len(store.inserted) != 2 {
		t.Fatalf("expected 2 events, got %d", len(store.inserted))
	}
	// First: only the type overridden, topic/key keep the registered defaults.
	e0 := store.inserted[0]
	if e0.Type != "amount.promo.created" || e0.Topic != "push-gateway" || e0.Key != "push-gateway.send" {
		t.Errorf("event 0: type=%q topic=%q key=%q", e0.Type, e0.Topic, e0.Key)
	}
	// Second: all three overridden.
	e1 := store.inserted[1]
	if e1.Type != "percentage.promo.created" || e1.Topic != "other" || e1.Key != "k2" {
		t.Errorf("event 1: type=%q topic=%q key=%q", e1.Type, e1.Topic, e1.Key)
	}
}

func TestRegistryPublishPointerResolvesSameRoute(t *testing.T) {
	reg := NewRegistry()
	Register[widgetCreated](reg, "widget.created", "widgets")

	store := &fakeStore{}
	pub := Bind(nil, store, reg)

	// Emitting a pointer to the registered value type must resolve the same route.
	if err := pub.Publish(context.Background(), &widgetCreated{ID: "7"}); err != nil {
		t.Fatalf("publish pointer: %v", err)
	}
	if len(store.inserted) != 1 || store.inserted[0].Topic != "widgets" {
		t.Fatalf("pointer did not resolve to the registered route: %+v", store.inserted)
	}
}

func TestRegistryPublishUnregisteredFails(t *testing.T) {
	reg := NewRegistry()
	store := &fakeStore{}
	pub := Bind(nil, store, reg)

	err := pub.Publish(context.Background(), widgetCreated{})
	if err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("expected not-registered error, got %v", err)
	}
	if len(store.inserted) != 0 {
		t.Error("nothing should be inserted for an unregistered type")
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	reg := NewRegistry()
	Register[widgetCreated](reg, "widget.created", "widgets")
	defer func() {
		if recover() == nil {
			t.Error("expected panic on duplicate registration")
		}
	}()
	Register[widgetCreated](reg, "widget.created", "widgets")
}

func TestWithContentTypeAndMarshaler(t *testing.T) {
	reg := NewRegistry()
	Register[widgetCreated](reg, "widget.created", "widgets",
		WithContentType("application/x-custom"),
		WithMarshaler(func(any) ([]byte, error) { return []byte("RAW"), nil }))

	store := &fakeStore{}
	pub := Bind(nil, store, reg)
	if err := pub.Publish(context.Background(), widgetCreated{}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	ev := store.inserted[0]
	if ev.ContentType != "application/x-custom" {
		t.Errorf("content type = %q", ev.ContentType)
	}
	if string(ev.Payload) != "RAW" {
		t.Errorf("payload = %q, want RAW", ev.Payload)
	}
}
