// Package logger builds the service's *slog.Logger: a JSON handler chain that
// correlates every record with the active trace, tags it with the service
// identity, and can fire side-effect hooks per level.
//
// New and NewWithHandler return a plain *slog.Logger, so the same logger is
// used everywhere — HTTP/gRPC handlers, business code, background workers and
// SDK packages — and third-party code logs through the same handler.
// BuildInfo logs the binary's embedded VCS/build metadata at startup.
//
// # Usage
//
// Build one logger at startup and inject it:
//
//	log := logger.New(os.Stdout, logger.Config{
//		Service:   "myapp", // service.name
//		Version:   version, // service.version — the build version
//		Level:     logger.LevelInfo,
//		AddSource: true,
//	})
//	log.InfoContext(ctx, "service started", "addr", ":8080")
//
//	access := log.With("logger", "access") // child tagged logger=access
//	access.InfoContext(ctx, "request", "method", "GET", "path", "/")
//
// Always log with a context (InfoContext, ErrorContext, ...; the sloglint
// linter enforces it): the context carries cancellation and the span the
// record is correlated with.
//
// # Correlation and identity
//
// When the context carries a valid OpenTelemetry span context, every record
// gets trace_id, span_id and trace_flags at its top level — outside any
// WithGroup — so a collector can link the line to its trace. Without one the
// fields are omitted. Config.Service and Config.Version are written as
// service.name and service.version, the OpenTelemetry resource attribute
// names; an empty Version is omitted.
//
// # Multiple sinks
//
// To split output by level, combine handlers with slog.NewMultiHandler and
// pass it to NewWithHandler. Each wrapped handler's own Enabled check gates
// which records it receives:
//
//	stdout := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logger.LevelInfo})
//	h := slog.NewMultiHandler(stdout, sentryHandler) // sentryHandler.Enabled gates ERROR+
//	log := logger.NewWithHandler(h, logger.Config{Service: "myapp", Version: version})
//
// # Side-effect hooks (Events)
//
// Events attaches a per-level callback that fires IN ADDITION to normal
// handling — the record is still written to every sink. Use it for side effects
// like alerting or Sentry capture on ERROR, without disturbing stdout logging:
//
//	log := logger.New(os.Stdout, logger.Config{
//		Service: "myapp",
//		Level:   logger.LevelInfo,
//		Events: logger.Events{
//			Error: func(ctx context.Context, r slog.Record) {
//				alert.Capture(ctx, r) // r carries service.*, trace fields and With attrs
//			},
//		},
//	})
//
// The callback receives the full slog.Record with the accumulated With and
// service.* attributes and the trace fields replayed in, so it sees the same
// context the sink writes. It runs synchronously before the write, so keep it
// fast (offload slow work to a goroutine) and do not log at the same level from
// within it (that re-enters the hook).
//
// Events is a hook, not a router: it is additive to whatever sink you built. To
// send full records to several destinations by level, use slog.NewMultiHandler
// (above); the two combine — a multi-handler sink plus an Error hook.
//
// # Conventions
//
//   - Errors go under the key "err" with an error value: log.ErrorContext(ctx,
//     "save failed", "err", err).
//   - Custom levels stay within [-8, 15], the range OpenTelemetry severity
//     numbers map to.
//
// # Config
//
// Config fields:
//   - Service: written as "service.name" on every record.
//   - Version: written as "service.version"; empty omits it.
//   - Level: minimum level handled (LevelDebug/LevelInfo/LevelWarn/LevelError).
//   - AddSource: attach a "source" attribute shortened to file:line.
//   - Events: optional per-level side-effect hook (see above); zero = none.
//
// New applies Level and AddSource to its JSON handler; NewWithHandler leaves
// both to the handler it is given.
package logger
