package outbox

import (
	"context"
	"log/slog"
	"testing"
	"time"
	"uuid"

	"github.com/matryer/is"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/otel"
	"github.com/assanoff/skit/otel/setup"
)

// tracedStore opens a span on every lease and mark, as a store on a traced
// database does, and leases the inserted events once. After a lease that
// finds nothing it signals drained.
type tracedStore struct {
	fakeStore

	tracer  trace.Tracer
	drained chan struct{}
}

func newTracedStore(tel *setup.Telemetry) *tracedStore {
	return &tracedStore{
		tracer:  tel.TracerProvider().Tracer("store"),
		drained: make(chan struct{}, 1),
	}
}

func (s *tracedStore) LeasePending(ctx context.Context, _ time.Time, _ int) ([]Event, error) {
	_, span := s.tracer.Start(ctx, "lease")
	span.End()
	events := s.inserted
	s.inserted = nil
	if len(events) == 0 {
		select {
		case s.drained <- struct{}{}:
		default:
		}
	}
	return events, nil
}

func (s *tracedStore) MarkSent(ctx context.Context, _ Event, _ uuid.UUID, _ time.Time) error {
	_, span := s.tracer.Start(ctx, "mark")
	span.End()
	return nil
}

// tracedPublisher records every send as the broker adapters do: a span under
// the creation context in the headers of the message.
type tracedPublisher struct{ tracer trace.Tracer }

func (p tracedPublisher) Publish(ctx context.Context, m broker.Message) error {
	_, span := p.tracer.Start(otel.ExtractFromCarrier(ctx, m.Headers), "send "+m.Topic)
	span.End()
	return nil
}

func (tracedPublisher) Close() error { return nil }

// relayTelemetry returns the telemetry of a process with the default sampler
// of setup, and the exporter of its spans.
func relayTelemetry(t *testing.T) (*setup.Telemetry, *tracetest.InMemoryExporter) {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tel, err := setup.New(t.Context(), slog.New(slog.DiscardHandler), setup.Config{
		Sampler:  sdktrace.ParentBased(sdktrace.AlwaysSample()),
		Exporter: exp,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() { _ = tel.Shutdown() })
	return tel, exp
}

// runRelay runs the relay on store until a tick finds nothing to send, then
// stops it and flushes the spans to the exporter. It flushes rather than shuts
// down: the in-memory exporter forgets its spans on Shutdown.
func runRelay(t *testing.T, tel *setup.Telemetry, store *tracedStore) {
	t.Helper()
	pub := tracedPublisher{tracer: tel.TracerProvider().Tracer("broker")}
	relay := NewRelay(nil, store, pub, RelayConfig{PollInterval: time.Millisecond})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- relay.Start(ctx) }()

	<-store.drained
	cancel()
	<-done
	tp := tel.TracerProvider().(*sdktrace.TracerProvider)
	if err := tp.ForceFlush(t.Context()); err != nil {
		t.Fatalf("flush: %v", err)
	}
}

// An idle relay exports nothing: its tick runs suppressed, so the lease of the
// store records no span.
func TestRelayEmptyTickExportsNothing(t *testing.T) {
	is := is.New(t)
	tel, exp := relayTelemetry(t)

	runRelay(t, tel, newTracedStore(tel))

	is.Equal(len(exp.GetSpans()), 0)
}

// The relay sends an event in the trace it was created in: the send is a child
// of the span that published the event, while the lease and the mark stay
// suppressed.
func TestRelaySendsInCreationTrace(t *testing.T) {
	is := is.New(t)
	tel, exp := relayTelemetry(t)
	store := newTracedStore(tel)
	reg := NewRegistry()
	mustRegister[widgetCreated](t, reg, "widget.created", "widgets")
	ctx, request := tel.TracerProvider().Tracer("api").Start(t.Context(), "request")
	is.NoErr(Bind(nil, store, reg).Publish(ctx, widgetCreated{ID: "42"}))
	request.End()

	runRelay(t, tel, store)

	spans := exp.GetSpans()
	is.Equal(len(spans), 2) // the request and the send: no lease, no mark
	is.Equal(spans[0].Name, "request")
	is.Equal(spans[1].Name, "send widgets")
	is.Equal(spans[1].Parent.SpanID(), spans[0].SpanContext.SpanID())
	is.Equal(spans[1].SpanContext.TraceID(), spans[0].SpanContext.TraceID())
}
