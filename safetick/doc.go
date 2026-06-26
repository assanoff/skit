// Package safetick provides panic recovery helpers for long-running background
// loops and message-consumer callbacks, so a single bad tick or message cannot
// crash the worker.
//
// RecoverTick is a deferred guard for the top of a tick; Guard wraps an inline
// call and reports whether it panicked. Both log the panic with its stack and
// invoke an optional PanicHandler (e.g. to bump a metric). This is the recovery
// primitive used by worker.Loop and poller.Poller.
//
// # Usage
//
// Defer RecoverTick at the start of any tick that must not bring down its loop:
//
//	func (w *W) tick(ctx context.Context) {
//	    defer safetick.RecoverTick(w.log, "outbox-relay", w.onPanic)
//	    // ... work that must not crash the loop ...
//	}
//
// When you need the recovered signal inline rather than via defer, use Guard:
//
//	if safetick.Guard(log, "consumer", onPanic, func() { handle(msg) }) {
//	    // the call panicked and was recovered; nack and move on
//	}
//
// # API
//
//   - PanicHandler: func(worker, phase string), invoked on recovery; phase is a
//     free-form label such as "tick", "handle", or "guard".
//   - RecoverTick(log, worker, onPanic): deferred tick guard.
//   - Guard(log, worker, onPanic, fn): run fn, recover any panic, return
//     whether one occurred.
package safetick
