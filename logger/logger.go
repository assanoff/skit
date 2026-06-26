package logger

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
)

// Level mirrors slog.Level for convenience.
type Level = slog.Level

// Log levels, re-exported from slog for convenience.
const (
	LevelDebug = slog.LevelDebug
	LevelInfo  = slog.LevelInfo
	LevelWarn  = slog.LevelWarn
	LevelError = slog.LevelError
)

// TraceIDFn extracts a trace id from the context. Return "" when none exists.
type TraceIDFn func(ctx context.Context) string

// Logger is the application logger.
type Logger struct {
	handler slog.Handler
}

// Config configures a Logger.
type Config struct {
	// Service tags every record with this name (attribute "service").
	Service string
	// Level is the minimum level handled.
	Level Level
	// AddSource attaches a "source" attribute (file:line) to records.
	AddSource bool
	// TraceIDFn injects "trace_id"; may be nil.
	TraceIDFn TraceIDFn
}

// New builds a Logger writing JSON to w. For multi-sink behavior, build the
// Logger with NewWithHandler and a FanoutHandler instead.
func New(w io.Writer, cfg Config) *Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       cfg.Level,
		AddSource:   cfg.AddSource,
		ReplaceAttr: replaceAttr,
	})
	return NewWithHandler(h, cfg)
}

// NewWithHandler builds a Logger from a custom slog.Handler (e.g. a
// FanoutHandler). The service attribute and TraceIDFn from cfg are applied.
//
// Trace-id injection is installed as a handler wrapper (not in the Logger's
// write path), so it also applies to any *slog.Logger derived via Slog() and to
// third-party middleware that logs through the same handler (e.g. httplog).
func NewWithHandler(h slog.Handler, cfg Config) *Logger {
	if cfg.Service != "" {
		h = h.WithAttrs([]slog.Attr{slog.String("service", cfg.Service)})
	}
	if cfg.TraceIDFn != nil {
		h = &traceHandler{Handler: h, fn: cfg.TraceIDFn}
	}
	return &Logger{handler: h}
}

// Handler returns the underlying slog.Handler, so callers can build a stdlib
// *slog.Logger that shares the same sink and formatting.
func (l *Logger) Handler() slog.Handler { return l.handler }

// Slog returns a stdlib *slog.Logger backed by the same handler.
func (l *Logger) Slog() *slog.Logger { return slog.New(l.handler) }

// Debug logs at debug level.
func (l *Logger) Debug(ctx context.Context, msg string, args ...any) {
	l.log(ctx, LevelDebug, msg, args...)
}

// Info logs at info level.
func (l *Logger) Info(ctx context.Context, msg string, args ...any) {
	l.log(ctx, LevelInfo, msg, args...)
}

// Warn logs at warn level.
func (l *Logger) Warn(ctx context.Context, msg string, args ...any) {
	l.log(ctx, LevelWarn, msg, args...)
}

func (l *Logger) Error(ctx context.Context, msg string, args ...any) {
	l.log(ctx, LevelError, msg, args...)
}

// Named returns a child Logger tagged with logger=name. Use it to derive
// purpose-specific instances (e.g. an access logger, a worker logger) that
// share the same underlying handler — so output, level, and any multi-sink
// fan-out (stdout + Sentry, ...) stay unified — while remaining
// distinguishable in the logs. This is the intended way to have several logger
// instances without proliferating handlers/config.
func (l *Logger) Named(name string) *Logger {
	return l.With("logger", name)
}

// With returns a child Logger whose records always carry the given attributes.
// The child shares the parent's handler.
func (l *Logger) With(args ...any) *Logger {
	rec := slog.NewRecord(zeroTime, 0, "", 0)
	rec.Add(args...)
	attrs := make([]slog.Attr, 0, rec.NumAttrs())
	rec.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})
	return &Logger{handler: l.handler.WithAttrs(attrs)}
}

func (l *Logger) log(ctx context.Context, level Level, msg string, args ...any) {
	if !l.handler.Enabled(ctx, level) {
		return
	}
	rec := slog.NewRecord(now(), level, msg, pc(3))
	rec.Add(args...)
	// trace_id is injected by the handler wrapper (see traceHandler), so it
	// applies uniformly here and via Slog().
	_ = l.handler.Handle(ctx, rec)
}

// replaceAttr shortens the source attribute to "file:line".
func replaceAttr(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.SourceKey {
		if src, ok := a.Value.Any().(*slog.Source); ok {
			a.Value = slog.StringValue(filepath.Base(src.File) + ":" + itoa(src.Line))
		}
	}
	return a
}
