// Package orderapp is the domain of the orders service in the outbox example.
// It sees neither dbx nor a transaction nor queue: the store opens the
// transaction and hands the callback a Repo bound to it, and with it a
// Scheduler that schedules tasks — the events among them — in the same
// transaction.
//
// # Usage
//
//	core := orderapp.New(store) // store: *orderdb.Store over the pool
//	err := core.Create(ctx, orderapp.Order{ID: "o-1", Total: 1500})
//
// Create writes the order and schedules the event OrderCreated, which commit
// together or not at all. The queue of the event, the kind of its task and
// the topic are chosen by the assembly.
package orderapp
