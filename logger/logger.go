package logger

import (
	"io"
	"log/slog"
	"path/filepath"
	"strconv"
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

// Config configures the logger built by New or NewWithHandler.
type Config struct {
	// Service tags every record with the service name (attribute
	// "service.name").
	Service string
	// Version tags every record with the build version (attribute
	// "service.version"); empty omits it.
	Version string
	// Level is the minimum level handled. New applies it; NewWithHandler
	// leaves level filtering to the given handler.
	Level Level
	// AddSource attaches a "source" attribute (file:line) to records. New
	// applies it; NewWithHandler leaves it to the given handler.
	AddSource bool
	// Events assigns an optional side-effect callback per level (e.g. fire an
	// alert on Error). It is additive: the record is still written to the sink.
	// Zero value installs no hook. See Events.
	Events Events
}

// New builds a logger writing JSON to w. For several sinks, build the logger
// with NewWithHandler over slog.NewMultiHandler instead. A logger over
// io.Discard drops every record before it is built and fires no Events hook.
func New(w io.Writer, cfg Config) *slog.Logger {
	if w == io.Discard {
		return slog.New(slog.DiscardHandler)
	}
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       cfg.Level,
		AddSource:   cfg.AddSource,
		ReplaceAttr: replaceAttr,
	})
	return NewWithHandler(h, cfg)
}

// NewWithHandler builds a logger over a custom slog.Handler (e.g. one made by
// slog.NewMultiHandler). The service identity and Events from cfg are applied.
//
// Everything is installed at the handler layer, so it applies to every logger
// derived with With/WithGroup and to third-party code logging through the same
// handler (e.g. httplog). The handler stack, innermost (closest to the sink)
// first, is:
//
//	h (the given sink)
//	└─ eventHandler   (if cfg.Events is set: side-effect hooks, additive)
//	   └─ WithAttrs(service.name, service.version)
//	      └─ traceHandler (trace_id, span_id, trace_flags; see below)
//
// traceHandler reads the span context from the ctx of each call and, when it
// is valid, adds trace_id, span_id and trace_flags at the top level of the
// record, outside any WithGroup. The event hook sits below service and trace so
// its callback sees the record with the full context it is written with.
func NewWithHandler(h slog.Handler, cfg Config) *slog.Logger {
	if cfg.Events.any() {
		h = newEventHandler(h, cfg.Events)
	}
	if attrs := identity(cfg); len(attrs) > 0 {
		h = h.WithAttrs(attrs)
	}
	return slog.New(&traceHandler{next: h})
}

// identity returns the service.* attributes set in cfg.
func identity(cfg Config) []slog.Attr {
	var attrs []slog.Attr
	if cfg.Service != "" {
		attrs = append(attrs, slog.String("service.name", cfg.Service))
	}
	if cfg.Version != "" {
		attrs = append(attrs, slog.String("service.version", cfg.Version))
	}
	return attrs
}

// replaceAttr shortens the source attribute to "file:line".
func replaceAttr(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.SourceKey {
		if src, ok := a.Value.Any().(*slog.Source); ok {
			a.Value = slog.StringValue(filepath.Base(src.File) + ":" + strconv.Itoa(src.Line))
		}
	}
	return a
}
