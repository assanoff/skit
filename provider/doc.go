// Package provider holds ready-made resource constructors for the dependencies
// applications need most often, so an app's deps layer declares WHAT to build
// and reuses these for HOW. Each returns a dim factory — func(ctx) (value,
// dim.CleanupFunc, error) — ready for dim.NewResource, built on top of the SDK's
// own primitives (dbx, broker/rabbitmq, otel).
//
// # Usage
//
//	// in an app initializer (app.Initializer[Deps]):
//	c.DB, cleanup = dim.NewResource("DB", provider.Postgres(dbx.Config{
//		User: opts.DB.User, Password: opts.DB.Password,
//		Host: opts.DB.Host, Name: opts.DB.Name, DisableTLS: opts.DB.DisableTLS,
//	}))
//	return cleanup, nil
//
// Each provider takes an SDK config (dbx.Config, rabbitmq.Config, otel.Config),
// so the app maps its own options to the SDK config once and the connect /
// status-check / cleanup boilerplate lives here.
//
// # Included
//
//   - Postgres: pgx pool via dbx.Open + StatusCheck; cleanup closes the pool.
//   - RabbitMQ: a *rabbitmq.Conn via rabbitmq.Dial; cleanup closes it.
//   - Redis: a *redis.Client (go-redis) verified with PING; cleanup closes it.
//   - Sentry: initializes the Sentry SDK and returns the Hub; cleanup flushes.
//   - Tracer: an OTLP/gRPC trace.Tracer via otel.InitTracing; cleanup flushes
//     and shuts down (gate it on your own enabled flag — see Tracer).
//   - Translator: an i18n.Translator built from caller-supplied message files;
//     language-agnostic (the app passes its default language + embed.FS).
//
// A SQLite provider is intentionally NOT here yet: dbx is Postgres-only (pgx),
// so it would pull a new SQLite driver dependency into the core SDK. Add it once
// that trade-off is decided (possibly as a sub-module).
package provider
