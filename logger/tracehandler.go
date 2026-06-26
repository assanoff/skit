package logger

import (
	"context"
	"log/slog"
)

// traceHandler is a slog.Handler middleware that injects a trace_id attribute
// (resolved from the context via fn) into every record. Installing it at the
// handler layer means the trace id appears regardless of how the record is
// produced — through *Logger or through a plain *slog.Logger obtained from
// Slog() — keeping logs consistent across the app and any third-party
// middleware sharing the handler.
type traceHandler struct {
	slog.Handler
	fn TraceIDFn
}

func (h *traceHandler) Handle(ctx context.Context, r slog.Record) error {
	if h.fn != nil {
		if id := h.fn(ctx); id != "" {
			r = r.Clone()
			r.AddAttrs(slog.String("trace_id", id))
		}
	}
	return h.Handler.Handle(ctx, r)
}

func (h *traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &traceHandler{Handler: h.Handler.WithAttrs(attrs), fn: h.fn}
}

func (h *traceHandler) WithGroup(name string) slog.Handler {
	return &traceHandler{Handler: h.Handler.WithGroup(name), fn: h.fn}
}
