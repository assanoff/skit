package outbox

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"uuid"

	"go.opentelemetry.io/otel/trace"

	"github.com/assanoff/skit/dbx"
)

// fakeStore is a minimal Store that records inserted events; only WithTx and
// Insert are exercised by the producer-side tests.
type fakeStore struct{ inserted []Event }

func (s *fakeStore) WithTx(dbx.Querier) Store { return s }
func (s *fakeStore) Insert(_ context.Context, events ...Event) error {
	s.inserted = append(s.inserted, events...)
	return nil
}

func (s *fakeStore) LeasePending(context.Context, time.Time, int) ([]Event, error) {
	return nil, nil
}

func (s *fakeStore) MarkSent(context.Context, Event, uuid.UUID, time.Time) error {
	return nil
}

func (s *fakeStore) MarkFailed(context.Context, Event, uuid.UUID, string, bool, time.Time) error {
	return nil
}

func (s *fakeStore) Release(context.Context, uuid.UUID) (int64, error) {
	return 0, nil
}

func (s *fakeStore) SweepExpiredLeases(context.Context, time.Duration, time.Time, int) (int64, error) {
	return 0, nil
}

func (s *fakeStore) Cleanup(context.Context, time.Duration, time.Time) (int64, error) {
	return 0, nil
}

type widgetCreated struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// mustRegister registers T's route and fails the test on a wiring error, so the
// setup in the success-path tests stays a single line.
func mustRegister[T any](t *testing.T, r *Registry, eventType, topic string) {
	t.Helper()
	if err := r.Register[T](eventType, topic); err != nil {
		t.Fatalf("register %s: %v", eventType, err)
	}
}

func TestRegistryPublishResolvesRoute(t *testing.T) {
	reg := NewRegistry()
	mustRegister[widgetCreated](t, reg, "widget.created", "widgets")

	store := &fakeStore{}
	pub := Bind(nil, store, reg)

	if err := pub.Publish(context.Background(), widgetCreated{ID: "42", Name: "gadget"}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(store.inserted) != 1 {
		t.Fatalf("expected 1 inserted event, got %d", len(store.inserted))
	}
	ev := store.inserted[0]
	if ev.Type != "widget.created" || ev.Topic != "widgets" {
		t.Errorf("route mismatch: type=%q topic=%q", ev.Type, ev.Topic)
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
	reg := NewRegistry()
	mustRegister[widgetCreated](t, reg, "widget.created", "widgets")
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
	mustRegister[widgetCreated](t, reg, "widget.created", "widgets")
	store := &fakeStore{}
	pub := Bind(nil, store, reg)

	if err := pub.Publish(context.Background(), widgetCreated{ID: "1"}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if h := string(store.inserted[0].Headers); h != "{}" {
		t.Errorf("headers = %q, want %q (no trace context to inject)", h, "{}")
	}
}

// amountCreated and percentageCreated share the payload of widgetCreated: one
// Go type per event type is how one payload goes out as several events.
type (
	amountCreated     widgetCreated
	percentageCreated widgetCreated
)

func TestTypesSharingAPayloadResolveTheirRoutes(t *testing.T) {
	reg := NewRegistry()
	mustRegister[amountCreated](t, reg, "amount.promo.created", "push-gateway")
	mustRegister[percentageCreated](t, reg, "percentage.promo.created", "other")

	store := &fakeStore{}
	pub := Bind(nil, store, reg)
	if err := pub.Publish(context.Background(), amountCreated{ID: "1"}); err != nil {
		t.Fatalf("publish amount: %v", err)
	}
	if err := pub.Publish(context.Background(), percentageCreated{ID: "2"}); err != nil {
		t.Fatalf("publish percentage: %v", err)
	}

	if len(store.inserted) != 2 {
		t.Fatalf("expected 2 events, got %d", len(store.inserted))
	}
	e0, e1 := store.inserted[0], store.inserted[1]
	if e0.Type != "amount.promo.created" || e0.Topic != "push-gateway" {
		t.Errorf("event 0: type=%q topic=%q", e0.Type, e0.Topic)
	}
	if e1.Type != "percentage.promo.created" || e1.Topic != "other" {
		t.Errorf("event 1: type=%q topic=%q", e1.Type, e1.Topic)
	}
}

func TestRegistryPublishPointerResolvesSameRoute(t *testing.T) {
	reg := NewRegistry()
	mustRegister[widgetCreated](t, reg, "widget.created", "widgets")

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

func TestRegisterDuplicateErrors(t *testing.T) {
	reg := NewRegistry()
	mustRegister[widgetCreated](t, reg, "widget.created", "widgets")

	err := reg.Register[widgetCreated]("widget.created", "widgets")
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("expected duplicate-registration error, got %v", err)
	}
}

func TestRegisterValidationErrors(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register[widgetCreated]("", "widgets"); err == nil {
		t.Error("expected an error for an empty event type")
	}
	if err := reg.Register[widgetCreated]("widget.created", ""); err == nil {
		t.Error("expected an error for an empty topic")
	}
}

// rawCodec stores every event as the same bytes.
type rawCodec struct{}

func (rawCodec) ContentType() string { return "application/x-custom" }

func (rawCodec) Marshal(any) ([]byte, error) { return []byte("RAW"), nil }

func TestWithCodec(t *testing.T) {
	reg := NewRegistry(WithCodec(rawCodec{}))
	mustRegister[widgetCreated](t, reg, "widget.created", "widgets")

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
