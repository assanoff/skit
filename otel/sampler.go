package otel

import (
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// endpointExcluder is a Sampler that never samples excluded routes and applies a
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
	if _, ok := e.endpoints[p.Name]; ok {
		return sdktrace.SamplingResult{Decision: sdktrace.Drop}
	}
	return e.inner.ShouldSample(p)
}

func (e endpointExcluder) Description() string {
	return "endpointExcluder{ratio+parentBased, route-exclusions}"
}
