package setup

import (
	"context"
	"log/slog"
)

// errorHandler logs the errors OTel reports, such as a failed export, at
// WARN: a failing collector must not wake the ERROR hooks (Sentry).
type errorHandler struct {
	log *slog.Logger
}

// Handle implements otel.ErrorHandler. OTel passes no context.
func (h errorHandler) Handle(err error) {
	h.log.WarnContext(context.Background(), "otel", "err", err)
}

// diagHandler lowers the levels of OTel's internal logger, which logr hands
// over as slog levels: Error and Warn (V(1)) go out at WARN, Info (V(4)) and
// Debug (V(8)) at DEBUG.
type diagHandler struct {
	next slog.Handler
}

// Enabled implements slog.Handler.
func (h diagHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, diagLevel(l))
}

// Handle implements slog.Handler.
func (h diagHandler) Handle(ctx context.Context, r slog.Record) error {
	r.Level = diagLevel(r.Level)
	return h.next.Handle(ctx, r)
}

// WithAttrs implements slog.Handler.
func (h diagHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return diagHandler{next: h.next.WithAttrs(attrs)}
}

// WithGroup implements slog.Handler.
func (h diagHandler) WithGroup(name string) slog.Handler {
	return diagHandler{next: h.next.WithGroup(name)}
}

// diagLevel maps a level of OTel's logger to the level it is written at.
func diagLevel(l slog.Level) slog.Level {
	if l >= -1 {
		return slog.LevelWarn
	}
	return slog.LevelDebug
}
