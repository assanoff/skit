// Package logger is a thin wrapper around log/slog tailored for services.
//
// It injects a trace_id into every record (via a pluggable TraceIDFn), tags
// records with the service name, exposes context-aware Debug/Info/Warn/Error
// methods, and can fan records out to multiple sinks by level (see
// FanoutHandler) — e.g. everything to stdout and ERROR+ to Sentry. The same
// *Logger is meant to be used everywhere: HTTP/gRPC handlers, business code,
// and background workers.
//
// # Usage
//
// Build one Logger at startup and inject it. Pair TraceIDFn with the otel
// package so every line carries the active trace id:
//
//	log := logger.New(os.Stdout, logger.Config{
//		Service:   "myapp",
//		Level:     logger.LevelInfo,
//		AddSource: true,
//		TraceIDFn: otel.GetTraceID,
//	})
//	log.Info(ctx, "service started", "addr", ":8080")
//
//	access := log.Named("access")   // child tagged logger=access
//	access.Info(ctx, "request", "method", "GET", "path", "/")
//
// # Multiple sinks
//
// To split output by level, build a FanoutHandler and pass it to
// NewWithHandler. Each wrapped handler's own Enabled check gates which records
// it receives, so per-sink level filtering lives on the handlers:
//
//	stdout := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logger.LevelInfo})
//	h := logger.NewFanout(stdout, sentryHandler) // sentryHandler.Enabled gates ERROR+
//	log := logger.NewWithHandler(h, logger.Config{Service: "myapp", TraceIDFn: otel.GetTraceID})
//
// # Interop with slog
//
// Handler returns the underlying slog.Handler and Slog returns a stdlib
// *slog.Logger backed by it, so third-party middleware (e.g. httplog) can log
// through the same sink, formatting, and trace-id injection. Trace-id injection
// is installed as a handler wrapper rather than in the *Logger write path, so it
// applies uniformly to *Logger, Slog(), and anything sharing the handler.
//
// # Config
//
// Config fields:
//   - Service: tags every record with attribute "service".
//   - Level: minimum level handled (LevelDebug/LevelInfo/LevelWarn/LevelError).
//   - AddSource: attach a "source" attribute shortened to file:line.
//   - TraceIDFn: injects "trace_id"; may be nil. Typically otel.GetTraceID.
//
// Deriving child loggers:
//   - Named(name): child tagged logger=name (access logger, worker logger, ...).
//   - With(args...): child whose records always carry the given attributes.
//
// Children share the parent's handler, so output, level, and any fan-out stay
// unified — the intended way to have several logger instances without
// proliferating handlers or config.
package logger
