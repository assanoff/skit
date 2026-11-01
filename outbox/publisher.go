package outbox

import (
	"context"
	"errors"
	"fmt"

	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/otel"
)

// Publisher records domain events into the outbox in the transaction it is
// bound to (Bind), or drops them (Discard). The domain declares its own
// interface with the same method — Publish(ctx, event any) error — and
// *Publisher satisfies it without an adapter, so the domain imports neither
// outbox nor dbx and knows nothing about the transaction or the transport.
//
// This is distinct from broker.Publisher: that one delivers an already-stored
// event to the wire (used by the Relay); this one writes the event into the
// outbox table inside the producer's transaction.
type Publisher struct {
	tx      *dbx.Tx
	ob      *PG
	reg     *Registry
	discard bool
}

// Bind returns a Publisher whose Publish writes events into the outbox table of
// ob on tx, resolving each event's route from reg. The events commit or roll
// back with the other writes in tx. A pool in place of tx does not compile: an
// event is never written outside the transaction of its domain change.
func Bind(tx *dbx.Tx, ob *PG, reg *Registry) *Publisher {
	return &Publisher{tx: tx, ob: ob, reg: reg}
}

// Discard returns a Publisher that drops every event: the build without a
// broker, and the default of a store built without events. The domain write
// goes through, the event goes nowhere, and the domain does not branch.
func Discard() *Publisher { return &Publisher{discard: true} }

// Publish records event in the outbox within the bound transaction. event's Go
// type must be registered: the Registry resolves its event type and topic and
// encodes it as JSON. Returns an error if the type is unregistered or
// encoding/insert fails. A Publisher from Discard returns nil.
func (p *Publisher) Publish(ctx context.Context, event any) error {
	if p.discard {
		return nil
	}
	ev, err := p.reg.event(ctx, event)
	if err != nil {
		return err
	}
	return p.ob.insert(ctx, p.tx, ev)
}

// event builds the outbox row for a registered value: its route, its payload
// encoded by the codec and the trace context of ctx in the headers.
func (r *Registry) event(ctx context.Context, v any) (Event, error) {
	if r == nil {
		return Event{}, errors.New("outbox: publish: no registry configured")
	}
	rt, err := r.lookup(v)
	if err != nil {
		return Event{}, err
	}
	codec := jsonCodec{}
	payload, err := codec.Marshal(v)
	if err != nil {
		return Event{}, fmt.Errorf("outbox: publish %s: marshal: %w", rt.eventType, err)
	}
	ct := codec.ContentType()
	ev, err := NewEvent(rt.eventType, rt.topic, ct, payload, traceHeaders(ctx))
	if err != nil {
		return Event{}, fmt.Errorf("outbox: publish %s: %w", rt.eventType, err)
	}
	return ev, nil
}

// traceHeaders captures the W3C trace context carried by ctx so it travels with
// the event: the relay copies the event's headers onto the broker message, and
// a consumer can restore the producer's span with otel.ExtractFromCarrier. It
// returns nil when ctx has no propagatable trace context, leaving headers empty.
func traceHeaders(ctx context.Context) map[string]any {
	carrier := otel.Carrier(ctx)
	if len(carrier) == 0 {
		return nil
	}
	h := make(map[string]any, len(carrier))
	for k, v := range carrier {
		h[k] = v
	}
	return h
}
