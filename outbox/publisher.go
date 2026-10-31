package outbox

import (
	"context"
	"errors"
	"fmt"

	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/otel"
)

// Publisher records domain events into the outbox. The domain depends only on
// this interface: it publishes a plain typed value and knows nothing about the
// transaction, the database, or the transport. A tx-bound Publisher is provided
// to the WithinTran closure (or built directly with Bind), so the recorded
// event commits atomically with the domain writes in the same transaction.
//
// This is distinct from broker.Publisher: that one delivers an already-stored
// event to the wire (used by the Relay); this one writes the event into the
// outbox table inside the producer's transaction.
type Publisher interface {
	// Publish records event in the outbox within the bound transaction. event's
	// Go type must be registered: the Registry resolves its event type and
	// topic and encodes it with its Codec. Returns an error if the type is
	// unregistered or encoding/insert fails.
	Publish(ctx context.Context, event any) error
}

// txPublisher is the tx-bound Publisher returned by Bind.
type txPublisher struct {
	store Store
	reg   *Registry
}

// Bind returns a Publisher whose Publish writes events into the outbox on tx,
// resolving each event's route from reg. Events recorded through it commit
// atomically with the other writes in tx.
//
// Usually you don't call Bind directly — WithinTran builds a bound Publisher
// and passes it to your closure. Bind is exposed for callers that manage the
// transaction themselves.
func Bind(tx dbx.Querier, store Store, reg *Registry) Publisher {
	return &txPublisher{store: store.WithTx(tx), reg: reg}
}

// Publish implements Publisher.
func (p *txPublisher) Publish(ctx context.Context, event any) error {
	if p.reg == nil {
		return errors.New("outbox: publish: no registry configured")
	}
	rt, err := p.reg.lookup(event)
	if err != nil {
		return err
	}
	payload, err := p.reg.codec.Marshal(event)
	if err != nil {
		return fmt.Errorf("outbox: publish %s: marshal: %w", rt.eventType, err)
	}
	ct := p.reg.codec.ContentType()
	ev, err := NewEvent(rt.eventType, rt.topic, ct, payload, traceHeaders(ctx))
	if err != nil {
		return fmt.Errorf("outbox: publish %s: %w", rt.eventType, err)
	}
	return p.store.Insert(ctx, ev)
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
