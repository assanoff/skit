package otel

import (
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// endpointExcluder is a Sampler that never samples spans started from a
// suppressed context (see Suppress) nor excluded routes, and applies a
// parent-based ratio sampler to everything else. The route is read from the
// span name (HTTP middleware names spans after the request path/route).
type endpointExcluder struct {
	endpoints map[string]struct{}
	inner     sdktrace.Sampler
}

func newEndpointExcluder(endpoints map[string]struct{}, probability float64) endpointExcluder {
	return endpointExcluder{
		endpoints: endpoints,
		inner:     sdktrace.ParentBased(sdktrace.TraceIDRatioBased(probability)),
	}
}

func (e endpointExcluder) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	if IsSuppressed(p.ParentContext) {
		return drop(p)
	}
	if _, ok := e.endpoints[p.Name]; ok {
		return drop(p)
	}
	return e.inner.ShouldSample(p)
}

func (e endpointExcluder) Description() string {
	return "endpointExcluder{suppressible, ratio+parentBased, route-exclusions}"
}

// drop returns a Drop decision that carries the parent tracestate forward:
// declining to record our own span must not destroy vendor state a downstream
// hop may still need.
func drop(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	res := sdktrace.SamplingResult{Decision: sdktrace.Drop}
	if p.ParentContext != nil {
		res.Tracestate = trace.SpanContextFromContext(p.ParentContext).TraceState()
	}
	return res
}
