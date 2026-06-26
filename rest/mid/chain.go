package mid

import (
	"context"

	"github.com/assanoff/skit/i18n"
	"github.com/assanoff/skit/logger"
	"github.com/assanoff/skit/rest"
)

// Config configures the standard application middleware chain built by Chain.
type Config struct {
	// Translator localizes *errs.Error responses; nil disables localization.
	Translator *i18n.Translator
	// Lang resolves the request language from the context (e.g. a reqctx
	// accessor). Required for localization; nil makes LocalizeErrors a no-op.
	Lang func(context.Context) string
	// Logger receives the span/log of 5xx errors (via Errors) and recovered
	// panics (via Panics); nil disables that logging.
	Logger *logger.Logger
	// MaskInternal hides 5xx error detail from clients (typically true in
	// production). See MaskInternal.
	MaskInternal bool
	// RecordMetric, when set, receives each request's outcome code (an
	// *errs.Error code, or "ok") for errs-aware metrics. See Metrics.
	RecordMetric func(code string)
}

// Chain returns the standard skit application middleware in order,
// outermost first: [Metrics] -> LocalizeErrors -> MaskInternal -> Errors ->
// Panics — a single, ordered chain that works with our *errs.Error types and
// the two-layer model. The order is deliberate: Panics (innermost) turns a
// handler panic into
// an Internal error; Errors logs/traces the original 5xx; MaskInternal then
// hides its detail from the client (it gets a nil logger here because Errors
// already logged); LocalizeErrors localizes the result; Metrics (outermost)
// counts the outcome.
//
// It is the SDK core only. Wrap it with your app's own middleware: put
// request-context parsing OUTSIDE it (so the language is in the context before
// LocalizeErrors reads it) and per-record translation INSIDE it, e.g.
//
//	mids := append([]rest.MidFunc{reqctx.Middleware()}, mid.Chain(cfg)...)
//	mids = append(mids, translationrest.MiddlewareWithLang(...))
//	r := router.New(mids...)
//
// Per-handler middleware (CacheControl, ETag, auth) are attached at the route,
// not here.
func Chain(cfg Config) []rest.MidFunc {
	mids := make([]rest.MidFunc, 0, 5)
	if cfg.RecordMetric != nil {
		mids = append(mids, Metrics(cfg.RecordMetric))
	}
	mids = append(
		mids,
		LocalizeErrors(cfg.Translator, cfg.Lang),
		MaskInternal(nil, cfg.MaskInternal), // Errors does the logging
		Errors(cfg.Logger),
		Panics(cfg.Logger),
	)
	return mids
}
