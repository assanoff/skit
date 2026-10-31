package safetick

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"

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

// PanicError is a panic that Call recovered: the value passed to panic and the
// stack of the goroutine that panicked.
type PanicError struct {
	Value any
	Stack []byte
}

// Error implements error: "panic: <value>".
func (e *PanicError) Error() string {
	return fmt.Sprintf("panic: %v", e.Value)
}

// Call runs fn and returns its error; a panic in fn is recovered and returned
// as a *PanicError instead, so the caller decides what the panic means. Call
// neither logs the panic nor counts it:
//
//	err := safetick.Call(func() error { return h.Handle(ctx, item) })
//	if pe, ok := errors.AsType[*safetick.PanicError](err); ok {
//	    log.Error("handler panic", "err", pe, "stack", string(pe.Stack))
//	}
func Call(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &PanicError{Value: r, Stack: debug.Stack()}
		}
	}()
	return fn()
}
