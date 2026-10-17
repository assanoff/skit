package middleware

import (
	"cmp"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"

	skotel "github.com/assanoff/skit/otel"
)

// TraceConfig configures Trace.
type TraceConfig struct {
	// TracerProvider records the server spans; nil → the global provider.
	TracerProvider trace.TracerProvider
	// Public marks the requests of an untrusted edge: their span starts a new
	// trace with a link to the caller's instead of continuing it. nil → every
	// caller is trusted.
	Public func(*http.Request) bool
}

// Trace opens the server span of a request with otelhttp and writes its
// traceparent into the response (otel.InjectToResponse). The span continues
// the W3C trace context of the request and is named "{METHOD} {route}".
//
// Put it into the group of business routes, after routing, so r.Pattern names
// the span, and first in that group: the access log, the metrics and the
// handler then run inside the span, and their log records carry its trace id.
// Routes outside the group (health, metrics, pprof) get no spans. HTTP metrics
// come from package metrics, so otelhttp records none.
func Trace(cfg TraceConfig) Middleware {
	opts := []otelhttp.Option{
		otelhttp.WithTracerProvider(cmp.Or(cfg.TracerProvider, otel.GetTracerProvider())),
		otelhttp.WithPropagators(skotel.Propagator()),
		otelhttp.WithMeterProvider(noop.NewMeterProvider()),
	}
	if cfg.Public != nil {
		opts = append(opts, otelhttp.WithPublicEndpointFn(cfg.Public))
	}
	serverSpan := otelhttp.NewMiddleware("http.server", opts...)
	return func(next http.Handler) http.Handler {
		return serverSpan(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			skotel.InjectToResponse(r.Context(), w.Header())
			next.ServeHTTP(w, r)
		}))
	}
}
