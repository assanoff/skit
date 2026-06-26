// Package middleware provides standard net/http server middleware for skit
// services.
//
// It covers the cross-cutting concerns most HTTP services need: panic recovery,
// trace-context injection, structured access logging, request timeouts, and
// body-size limits. Every middleware has the signature func(http.Handler)
// http.Handler (aliased as Middleware), so it composes with router.Use,
// router.With, and go-pkgz/routegroup. Because TraceRequest seeds a trace id and
// AccessLog logs through the skit logger, log lines carry trace_id
// automatically.
//
// These are the transport layer of the two-layer model (see the router package):
// they wrap the encoded response and every route — including raw http.Handlers
// and the typed rest boundary — via router.Use / router.With. Concerns that need
// the typed ResponseEncoder a handler returns (auth, validation, error localization)
// belong instead in the application layer as rest.MidFunc (router.UseApp /
// WithApp / per-route).
//
// # Ordering
//
// Register them outermost-first. Panics goes first so it wraps everything;
// TraceRequest next so downstream logs and spans share the request's trace
// context; then AccessLog, then the limit/timeout guards:
//
//	r := router.New(appMids...)
//	r.Use(
//		middleware.Panics(log),            // recover -> 500, log stack
//		middleware.TraceRequest(tracer),   // extract/seed W3C trace context
//		middleware.AccessLog(log),         // one structured line per request
//		middleware.SizeLimit(1<<20),       // cap request body at 1 MiB
//		middleware.Timeout(5*time.Second), // cancel ctx after 5s
//	)
//
// # Middleware
//
//   - Panics(log): recover from panics, log the stack, respond 500. A nil log
//     skips logging.
//   - TraceRequest(tracer): extract incoming W3C trace context and ensure a
//     trace id is available for logging.
//   - AccessLog(log): emit one structured line per request using OpenTelemetry
//     HTTP semantic-convention field names. A nil log skips logging.
//   - Timeout(d): cancel the request context after d; a non-positive d disables
//     it (returns next unchanged).
//   - SizeLimit(n): cap the request body at n bytes via http.MaxBytesReader; a
//     non-positive n disables it.
package middleware
