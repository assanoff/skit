package logger

import (
	"context"
	"log/slog"
)

// FanoutHandler dispatches each record to every wrapped handler whose own
// Enabled check passes. This lets you, for example, send all records to a
// stdout JSON handler while also forwarding ERROR+ records to a Sentry handler.
//
//	stdout := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: LevelInfo})
//	h := logger.NewFanout(stdout, sentryHandler) // sentryHandler.Enabled gates ERROR+
//	log := logger.NewWithHandler(h, logger.Config{Service: "svc", TraceIDFn: otel.GetTraceID})
type FanoutHandler struct {
	handlers []slog.Handler
}

// NewFanout builds a FanoutHandler over the given handlers (nil handlers are
// skipped). Per-sink level filtering is the responsibility of each handler.
func NewFanout(handlers ...slog.Handler) *FanoutHandler {
	hs := make([]slog.Handler, 0, len(handlers))
	for _, h := range handlers {
		if h != nil {
			hs = append(hs, h)
		}
	}
	return &FanoutHandler{handlers: hs}
}

// Enabled reports whether any wrapped handler is enabled for the level.
func (f *FanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range f.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle forwards the record to every wrapped handler that is enabled for it.
func (f *FanoutHandler) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, h := range f.handlers {
		if !h.Enabled(ctx, r.Level) {
			continue
		}
		if err := h.Handle(ctx, r.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// WithAttrs implements slog.Handler, fanning attrs out to each child handler.
func (f *FanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	hs := make([]slog.Handler, len(f.handlers))
	for i, h := range f.handlers {
		hs[i] = h.WithAttrs(attrs)
	}
	return &FanoutHandler{handlers: hs}
}

// WithGroup implements slog.Handler, fanning the group out to each child handler.
func (f *FanoutHandler) WithGroup(name string) slog.Handler {
	hs := make([]slog.Handler, len(f.handlers))
	for i, h := range f.handlers {
		hs[i] = h.WithGroup(name)
	}
	return &FanoutHandler{handlers: hs}
}
