package safetick

import (
	"log/slog"
	"runtime/debug"
)

// PanicHandler is called when a panic is recovered, e.g. to bump a metric.
// phase is a free-form label such as "tick" or "handle".
type PanicHandler func(worker, phase string)

// RecoverTick recovers a panic in a worker tick, logs it with the stack, and
// invokes onPanic (if non-nil). Use it as a deferred call at the top of a tick:
//
//	func (w *W) tick(ctx context.Context) {
//	    defer safetick.RecoverTick(w.log, "outbox-relay", w.onPanic)
//	    // ... work that must not crash the loop ...
//	}
func RecoverTick(log *slog.Logger, worker string, onPanic PanicHandler) {
	if r := recover(); r != nil {
		if onPanic != nil {
			onPanic(worker, "tick")
		}
		if log != nil {
			log.Error("worker tick panic recovered",
				"worker", worker,
				"panic", r,
				"stack", string(debug.Stack()),
			)
		}
	}
}

// Guard runs fn and converts a panic into a (logged) no-op, returning true if a
// panic was recovered. Handy when you need the recovered signal inline rather
// than via defer.
func Guard(log *slog.Logger, worker string, onPanic PanicHandler, fn func()) (recovered bool) {
	defer func() {
		if r := recover(); r != nil {
			recovered = true
			if onPanic != nil {
				onPanic(worker, "guard")
			}
			if log != nil {
				log.Error("guarded call panic recovered",
					"worker", worker, "panic", r, "stack", string(debug.Stack()))
			}
		}
	}()
	fn()
	return false
}
