// Package eventbus is an in-process, synchronous event bus for decoupling
// domain packages that cannot import one another (avoiding import cycles).
//
// A producer domain dispatches an event by (domain, action); consumer domains
// register handlers for the pairs they care about. The producer never imports
// the consumers — it only knows the event's name and payload — so dependencies
// flow one way. Handlers run synchronously on the dispatching goroutine.
//
// Choose eventbus for internal, in-process reactions that should happen as part
// of the same call (and whose failure should surface to the caller). It is NOT
// a message broker: it offers no durability, async delivery, retries, or
// cross-service transport — for those, use skit's outbox + broker. The
// two are complementary: a domain change can both fire an eventbus event for
// in-process side effects and write to the outbox for reliable external
// delivery.
//
// eventbus dispatches outside any transaction: handlers run after the
// producer's write. When you need a domain change and its reactions to commit
// atomically — or to survive a crash — use skit's outbox instead, which
// writes events in the same transaction as the domain change and delivers them
// reliably.
//
// # Usage
//
// At startup, construct a Bus and let consumers Register handlers; a handler
// recovers the typed payload with Decode. Producers build an event with NewData
// (or MustData) and dispatch it with Call or Publish:
//
//	bus := eventbus.New(log)
//
//	type UserCreated struct{ ID string }
//
//	// Consumer side (wired once, never imports the producer):
//	bus.Register("user", "created", func(ctx context.Context, d eventbus.Data) error {
//	    ev, err := eventbus.Decode[UserCreated](d)
//	    if err != nil {
//	        return err
//	    }
//	    return mailer.SendWelcome(ctx, ev.ID)
//	})
//
//	// Producer side (only knows the event name and payload):
//	evt := eventbus.MustData("user", "created", UserCreated{ID: u.ID})
//	if err := bus.Call(ctx, evt); err != nil { // abort on first handler error
//	    return err
//	}
//
// # Dispatch semantics
//
//   - Call    — run handlers in registration order, stop at and return the
//     first error. Use it when a side effect's failure must abort the producer.
//   - Publish — run every handler regardless of individual failures and return
//     their errors joined with errors.Join. Use it for independent, best-effort
//     notifications.
//
// A (domain, action) pair with no handlers is a no-op. The Bus is safe for
// concurrent use, and handlers are snapshotted before they run, so a handler may
// itself Register without deadlocking.
//
// # Events
//
// Data is the event: a Domain, an Action, and opaque RawParams (JSON by
// convention) so the bus stays decoupled from any concrete payload type. NewData
// JSON-encodes params (nil for a payload-less event) and returns an error;
// MustData panics instead, for static types that cannot fail to encode. A
// handler reconstructs the payload with the generic Decode[T].
package eventbus
