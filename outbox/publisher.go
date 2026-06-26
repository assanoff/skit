package outbox

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"

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
	// Go type must be registered; its route (topic, key, content type) and
	// marshaling are resolved from the Registry. Returns an error if the type is
	// unregistered or marshaling/insert fails.
	//
	// opts override the registered route per call. This is how one Go payload
	// type fans out to several CloudEvents types chosen at runtime: register the
	// type once with a default route, then pass As(...) (and optionally
	// OnTopic/WithRouteKey) to pick the concrete event for this publish.
	Publish(ctx context.Context, event any, opts ...PublishOption) error
}

// PublishOption overrides the registered route for a single Publish. The Go
// type still resolves marshaling and the route defaults from the Registry;
// these options replace individual fields for this call only.
type PublishOption func(*route)

// As overrides the CloudEvents type for this publish. Use it when one payload
// type maps to several event types selected by runtime conditions (e.g. a
// promocode whose discount kind decides amount/percentage/category.created).
func As(eventType string) PublishOption {
	return func(r *route) {
		if eventType != "" {
			r.eventType = eventType
		}
	}
}

// OnTopic overrides the destination topic for this publish.
func OnTopic(topic string) PublishOption {
	return func(r *route) {
		if topic != "" {
			r.topic = topic
		}
	}
}

// WithRouteKey overrides the routing/ordering key for this publish.
func WithRouteKey(key string) PublishOption {
	return func(r *route) { r.key = key }
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
func Bind(tx sqlx.ExtContext, store Store, reg *Registry) Publisher {
	return &txPublisher{store: store.WithTx(tx), reg: reg}
}

// Publish implements Publisher.
func (p *txPublisher) Publish(ctx context.Context, event any, opts ...PublishOption) error {
	if p.reg == nil {
		return fmt.Errorf("outbox: publish: no registry configured")
	}
	rt, err := p.reg.lookup(event)
	if err != nil {
		return err
	}
	for _, opt := range opts {
		opt(&rt)
	}
	payload, err := rt.marshal(event)
	if err != nil {
		return fmt.Errorf("outbox: publish %s: marshal: %w", rt.eventType, err)
	}
	ev, err := NewEvent(rt.eventType, rt.topic, rt.key, rt.contentType, payload, traceHeaders(ctx))
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
