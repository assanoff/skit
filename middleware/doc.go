// Package middleware provides standard net/http server middleware for skit
// services.
//
// It covers the cross-cutting concerns most HTTP services need: panic recovery,
// the server span, structured access logging, request timeouts, body-size
// limits, CORS, security headers, response compression, and request-id echo.
// Every middleware has the signature func(http.Handler) http.Handler (aliased
// as Middleware), so it composes with router.Use, router.With, and
// go-pkgz/routegroup. Because Trace puts the server span into the request
// context, an AccessLog over a logger built by package logger writes trace_id
// on every line.
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
// Panics goes on the root router, so it wraps everything. The rest wraps the
// business routes only, in a group made with router.With: Trace first, after
// routing, so the span is named after the route pattern; then AccessLog, then
// the limit/timeout guards. Technical routes — probes, metrics, pprof — stay
// outside the group: they get no span, no access-log line and no timeout.
//
//	r := router.New(appMids...)
//	r.Use(middleware.Panics(log)) // recover -> 500, log stack
//	api := r.With(
//		// server span per request, named after the route
//		middleware.Trace(middleware.TraceConfig{TracerProvider: tp}),
//		middleware.AccessLog(log),         // one structured line per request
//		middleware.SizeLimit(1<<20),       // cap request body at 1 MiB
//		middleware.Timeout(5*time.Second), // cancel ctx after 5s
//	)
//
// # Middleware
//
//   - Panics(log): recover from panics, log the stack, respond 500. A nil log
//     skips logging. http.ErrAbortHandler is panicked again for net/http.
//   - Trace(cfg): open the server span of the request with otelhttp, named
//     "{METHOD} {route}", continuing the incoming W3C trace context, and return
//     the span's traceparent in the response. TraceConfig.TracerProvider nil
//     means the global provider; TraceConfig.Public marks requests from outside
//     the trust boundary, whose span starts a new trace linked to the caller's.
//   - SetRoute(r, route): name the server span after route, for a handler that
//     routes by itself below Trace (a grpc-gateway mux mounted on a prefix).
//   - AccessLog(log): emit one structured line per request using OpenTelemetry
//     HTTP semantic-convention field names. A nil log skips logging.
//   - Timeout(d): cancel the request context after d; a non-positive d disables
//     it (returns next unchanged).
//   - SizeLimit(n): cap the request body at n bytes via http.MaxBytesReader; a
//     non-positive n disables it.
//   - CORS(cfg): apply a cross-origin policy — set Access-Control-* headers on
//     allowed origins and answer preflight OPTIONS with 204.
//   - SecureHeaders(): set conservative security response headers (nosniff,
//     frame-deny, no-referrer) without overriding ones a handler set.
//   - Compress(): gzip responses when the client sends Accept-Encoding: gzip.
//   - RequestID(): echo the correlation id (incoming X-Request-ID, else the
//     trace id) in the X-Request-ID response header.
//
// The last four are not part of the generated scaffold's default chain — wire
// them per service (CORS in particular needs a per-deployment origin policy).
package middleware
