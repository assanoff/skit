package safetick

import (
	"context"
	"log/slog"

	"github.com/assanoff/skit/logger"
)

// PanicHandler is called when a panic is recovered, e.g. to bump a metric.
// phase is a free-form label such as "tick" or "handle".
type PanicHandler func(worker, phase string)

// RecoverTick recovers a panic in a worker tick, logs it with the stack, and
// invokes onPanic (if non-nil). Use it as a deferred call at the top of a tick:
//
//	func (w *W) tick(ctx context.Context) {
//	    defer safetick.RecoverTick(ctx, w.log, "outbox-relay", w.onPanic)
//	    // ... work that must not crash the loop ...
//	}
func RecoverTick(ctx context.Context, log *slog.Logger, worker string, onPanic PanicHandler) {
	if r := recover(); r != nil { //nolint:revive // RecoverTick is itself the deferred call
		if onPanic != nil {
			onPanic(worker, "tick")
		}
		if log != nil {
			log.ErrorContext(ctx, "worker tick panic recovered",
				logger.PanicArgs(r, "worker", worker)...)
		}
	}
}

// Guard runs fn and converts a panic into a (logged) no-op, returning true if a
// panic was recovered. Handy when you need the recovered signal inline rather
// than via defer.
func Guard(ctx context.Context, log *slog.Logger, worker string, onPanic PanicHandler, fn func()) (recovered bool) {
	defer func() {
		if r := recover(); r != nil {
			recovered = true
			if onPanic != nil {
				onPanic(worker, "guard")
			}
			if log != nil {
				log.ErrorContext(ctx, "guarded call panic recovered",
					logger.PanicArgs(r, "worker", worker)...)
			}
		}
	}()
	fn()
	return false
}
