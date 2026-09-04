package otel

import (
	"context"
	"testing"

	"github.com/matryer/is"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestSuppressIsInherited(t *testing.T) {
	is := is.New(t)

	var nilCtx context.Context
	is.True(!IsSuppressed(nilCtx))               // a nil context is not suppressed
	is.True(!IsSuppressed(context.Background())) // a plain context is not suppressed

	ctx := Suppress(context.Background())
	is.True(IsSuppressed(ctx)) // round-trips

	child, cancel := context.WithCancel(ctx)
	defer cancel()
	is.True(IsSuppressed(child)) // every derived context stays suppressed
}

func TestEndpointExcluderDropsSuppressed(t *testing.T) {
	is := is.New(t)

	// Ratio 1.0 and no route exclusions: the only thing that can drop the span
	// is the suppression rule.
	ex := newEndpointExcluder(nil, 1.0)

	res := ex.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: Suppress(context.Background()),
		Name:          "poll-pending",
	})
	is.Equal(res.Decision, sdktrace.Drop) // background tick is not exported

	res = ex.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		Name:          "poll-pending",
	})
	is.Equal(res.Decision, sdktrace.RecordAndSample) // same name, unsuppressed -> traced
}

func TestDropPreservesTracestate(t *testing.T) {
	is := is.New(t)

	ts, err := trace.ParseTraceState("vendor=state")
	is.NoErr(err)

	traceID, err := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	is.NoErr(err)
	spanID, err := trace.SpanIDFromHex("0102030405060708")
	is.NoErr(err)

	parent := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		TraceState: ts,
	}))

	ex := newEndpointExcluder(map[string]struct{}{"/healthz": {}}, 1.0)

	suppressed := ex.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: Suppress(parent),
		Name:          "poll-pending",
	})
	is.Equal(suppressed.Decision, sdktrace.Drop)
	is.Equal(suppressed.Tracestate.Get("vendor"), "state") // vendor state survives the drop

	excluded := ex.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: parent,
		Name:          "/healthz",
	})
	is.Equal(excluded.Decision, sdktrace.Drop)
	is.Equal(excluded.Tracestate.Get("vendor"), "state") // ... on the route-exclusion path too
}
