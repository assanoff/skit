package middleware

import (
	"cmp"
	"context"
	"net/http"
	"strings"
	"sync/atomic"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric/noop"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
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

// routeKey is the context key of the route SetRoute records. Trace puts an
// empty cell into the context before otelhttp, so the cell is still there when
// otelhttp names the span after the handler.
type routeKey struct{}

// otherMethod is the method the HTTP semantic conventions record for a method
// they do not know.
const otherMethod = "_OTHER"

// Trace opens the server span of a request with otelhttp and writes its
// traceparent into the response (otel.InjectToResponse). The span continues
// the W3C trace context of the request and is named "{METHOD} {route}".
//
// Put it into the group of business routes, after routing, so r.Pattern names
// the span, and first in that group: the access log, the metrics and the
// handler then run inside the span, and their log records carry its trace id.
// Routes outside the group (health, metrics, pprof) get no spans. HTTP metrics
// come from package metrics, so otelhttp records none.
//
// A handler that routes by itself below Trace, such as a grpc-gateway mux
// mounted on a prefix, names the span after its own route with SetRoute.
func Trace(cfg TraceConfig) Middleware {
	opts := []otelhttp.Option{
		otelhttp.WithTracerProvider(cmp.Or(cfg.TracerProvider, otel.GetTracerProvider())),
		otelhttp.WithPropagators(skotel.Propagator()),
		otelhttp.WithMeterProvider(noop.NewMeterProvider()),
		otelhttp.WithSpanNameFormatter(spanName),
	}
	if cfg.Public != nil {
		opts = append(opts, otelhttp.WithPublicEndpointFn(cfg.Public))
	}
	serverSpan := otelhttp.NewMiddleware("http.server", opts...)
	return func(next http.Handler) http.Handler {
		h := serverSpan(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			skotel.InjectToResponse(r.Context(), w.Header())
			next.ServeHTTP(w, r)
		}))
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), routeKey{}, new(atomic.Pointer[string]))
			h.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// SetRoute names the server span of r after route, for a handler that routes
// by itself below Trace: the span becomes "{METHOD} {route}" with http.route
// set to route, instead of the prefix the outer router matched, and Route
// reports it, so the HTTP metrics label the request with it too. route is a
// template such as "/v1/widgets/{id}", never the request path. Safe to call
// from any goroutine of the handler.
func SetRoute(r *http.Request, route string) {
	if cell, ok := r.Context().Value(routeKey{}).(*atomic.Pointer[string]); ok {
		cell.Store(&route)
	}
	span := trace.SpanFromContext(r.Context())
	span.SetName(spanMethod(r) + " " + route)
	span.SetAttributes(semconv.HTTPRoute(route))
}

// Route returns the route of r as a template such as "/v1/widgets/{id}": the
// route SetRoute recorded, else r.Pattern without its method and host, else ""
// (a request outside a route group). The HTTP metrics label requests with it.
func Route(r *http.Request) string {
	return cmp.Or(recordedRoute(r.Context()), routePattern(r.Pattern))
}

// Method returns the method of r as the HTTP semantic conventions record it:
// one of the nine standard methods, upper-cased, else "_OTHER". The HTTP
// metrics label requests with it.
func Method(r *http.Request) string {
	m := strings.ToUpper(r.Method)
	switch m {
	case
		http.MethodConnect,
		http.MethodDelete,
		http.MethodGet,
		http.MethodHead,
		http.MethodOptions,
		http.MethodPatch,
		http.MethodPost,
		http.MethodPut,
		http.MethodTrace:

		return m
	}
	return otherMethod
}

// spanName names the server span as otelhttp does — "{METHOD} {route}", or the
// method alone without a route — but takes the route SetRoute recorded first,
// then r.Pattern. otelhttp names the span again after the handler whenever
// r.Pattern is set, and would otherwise put the prefix back.
func spanName(_ string, r *http.Request) string {
	route := Route(r)
	if route == "" {
		return spanMethod(r)
	}
	return spanMethod(r) + " " + route
}

// spanMethod is the request method as span names carry it: a method the HTTP
// semantic conventions do not know becomes "HTTP".
func spanMethod(r *http.Request) string {
	if m := Method(r); m != otherMethod {
		return m
	}
	return "HTTP"
}

// recordedRoute returns the route SetRoute recorded in ctx, or "".
func recordedRoute(ctx context.Context) string {
	cell, ok := ctx.Value(routeKey{}).(*atomic.Pointer[string])
	if !ok {
		return ""
	}
	if p := cell.Load(); p != nil {
		return *p
	}
	return ""
}

// routePattern drops the method and host of a ServeMux pattern:
// "GET example.com/x/{id}" → "/x/{id}".
func routePattern(pattern string) string {
	if i := strings.IndexByte(pattern, '/'); i >= 0 {
		return pattern[i:]
	}
	return ""
}
