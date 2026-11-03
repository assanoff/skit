// Package safetick provides panic recovery helpers for long-running background
// loops and message-consumer callbacks, so a single bad tick or message cannot
// crash the worker.
//
// RecoverTick is a deferred guard for the top of a tick; Guard wraps an inline
// call and reports whether it panicked. Both log the panic with its stack and
// invoke an optional PanicHandler (e.g. to bump a metric). Call returns the
// panic as an error instead and leaves the logging to the caller. worker,
// cron, poller, queue, auditlog and the broker consumers recover with them.
//
// # Usage
//
// Defer RecoverTick at the start of any tick that must not bring down its loop:
//
//	func (w *W) tick(ctx context.Context) {
//	    defer safetick.RecoverTick(ctx, w.log, "report-poller", w.onPanic)
//	    // ... work that must not crash the loop ...
//	}
//
// When you need the recovered signal inline rather than via defer, use Guard:
//
//	if safetick.Guard(ctx, log, "consumer", onPanic, func() { handle(msg) }) {
//	    // the call panicked and was recovered; nack and move on
//	}
//
// When the panic decides the outcome, such as a retry of a task, use Call:
//
//	err := safetick.Call(func() error { return h(ctx, item) })
//	if pe, ok := errors.AsType[*safetick.PanicError](err); ok {
//	    log.ErrorContext(ctx, "handler panic", "err", pe, "stack", string(pe.Stack))
//	}
//
// # API
//
//   - PanicHandler: func(worker, phase string), invoked on recovery; phase is a
//     free-form label such as "tick", "handle", or "guard".
//   - RecoverTick(ctx, log, worker, onPanic): deferred tick guard.
//   - Guard(ctx, log, worker, onPanic, fn): run fn, recover any panic, return
//     whether one occurred.
//   - Call(fn): run fn, return its error or the panic as a *PanicError (the
//     value passed to panic and the stack); no logging, no PanicHandler.
package safetick
