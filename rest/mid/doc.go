// Package mid provides application-layer middleware: rest.MidFunc values
// that wrap a typed rest.HandlerFunc and operate on the ResponseEncoder it returns (the
// typed value), not on raw bytes.
//
// It is the typed-layer twin of the root middleware package, which holds the
// net/http transport middleware (func(http.Handler) http.Handler). Apply these via router.New,
// router.UseApp, router.WithApp, or a per-route handle — never router.Use, which
// takes transport middleware. See the router package for the two-layer model and
// the Use/UseApp, With/WithApp, Handle/HandleApp method pairs.
//
// # Middleware
//
//   - Chain(Config): the standard application chain in order ([Metrics],
//     LocalizeErrors, MaskInternal, Errors, Panics) for our *errs.Error types.
//     Wrap it with your own request-context (outside) and translation (inside)
//     middleware.
//   - Panics(log): recover a panic from a typed handler into an Internal
//     *errs.Error so it flows through the pipeline (logged, masked, localized,
//     JSON-encoded) instead of a bare 500. Innermost of the chain.
//   - Errors(log): observe 5xx errors — record them on the active OpenTelemetry
//     span and log them server-side by domain code. Observability only (masking
//     is MaskInternal, localization is LocalizeErrors).
//   - Metrics(record): report each request's outcome code (errs code, or "ok")
//     to a backend-agnostic callback — errs-aware metrics by domain code.
//   - LocalizeErrors(tr, lang): translate an *errs.Error response into the
//     request language (resolved by the lang accessor) before it is encoded.
//     Place it outermost of the app middleware so it wraps and localizes errors
//     returned by deeper auth/validation middleware.
//   - MaskInternal(log, mask): when mask is set, log a 5xx *errs.Error
//     server-side and replace it with a detail-free generic error so internal
//     failures (DB messages, etc.) don't leak to clients. Place it inside
//     LocalizeErrors.
//   - CacheControl(maxAge, vary...): set Cache-Control/Vary on successful
//     responses. Attach per handler or group, the developer's choice.
//   - ETag(): add a strong ETag to successful responses and answer a matching
//     If-None-Match with 304. Attach per handler or group.
//
// CacheControl and ETag set response headers, which the typed ResponseEncoder cannot
// express, so they reach the ResponseWriter via rest.GetWriter (installed by the
// HandlerFunc.ServeHTTP boundary). They only set headers — never call
// WriteHeader — so Respond stays the single writer and they compose in any
// order. Off the boundary (a handler called directly) GetWriter is nil and they
// are no-ops.
package mid
