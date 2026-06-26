package otel

import (
	"context"
	"testing"

	"github.com/matryer/is"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestGetTraceID(t *testing.T) {
	is := is.New(t)

	is.Equal(GetTraceID(context.Background()), "") // no trace id stored -> empty

	ctx := setTraceID(context.Background(), "abc123")
	is.Equal(GetTraceID(ctx), "abc123") // round-trips the stored id
}

func TestEndpointExcluderDropsExcludedRoute(t *testing.T) {
	is := is.New(t)

	ex := newEndpointExcluder(map[string]struct{}{"/healthz": {}}, 1.0)

	dropped := ex.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		Name:          "/healthz",
	})
	is.Equal(dropped.Decision, sdktrace.Drop) // excluded route is never sampled

	// A non-excluded route falls through to the inner ratio sampler; at ratio 1.0
	// it always records.
	sampled := ex.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		Name:          "/widgets",
	})
	is.Equal(sampled.Decision, sdktrace.RecordAndSample)
}

func TestEndpointExcluderDescription(t *testing.T) {
	is := is.New(t)

	ex := newEndpointExcluder(nil, 0.5)
	is.True(ex.Description() != "") // a Sampler must describe itself
}
