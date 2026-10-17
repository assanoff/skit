package setup

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	skotel "github.com/assanoff/skit/otel"
)

const (
	envSampler    = "OTEL_TRACES_SAMPLER"
	envSamplerArg = "OTEL_TRACES_SAMPLER_ARG"
)

// sampler returns the sampler of the provider. Without an exporter nothing is
// sampled: the trace flags stay 00, so a downstream parent-based sampler does
// not record a trace this process never exports.
func sampler(ctx context.Context, log *slog.Logger, cfg Config) sdktrace.Sampler {
	if cfg.Exporter == nil {
		return sdktrace.NeverSample()
	}
	s := cfg.Sampler
	if s == nil {
		s = samplerFromEnv(ctx, log)
	}
	return suppressible{next: s}
}

// samplerFromEnv reads OTEL_TRACES_SAMPLER and OTEL_TRACES_SAMPLER_ARG as the
// SDK does; sdktrace.WithSampler, which Suppress needs, turns the SDK's own
// reading off. Unset → parentbased_always_on. An unsupported value is logged
// and falls back to the default.
func samplerFromEnv(ctx context.Context, log *slog.Logger) sdktrace.Sampler {
	name := strings.ToLower(strings.TrimSpace(os.Getenv(envSampler)))
	switch name {
	case "", "parentbased_always_on":
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	case "parentbased_always_off":
		return sdktrace.ParentBased(sdktrace.NeverSample())
	case "parentbased_traceidratio":
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio(ctx, log)))
	case "always_on":
		return sdktrace.AlwaysSample()
	case "always_off":
		return sdktrace.NeverSample()
	case "traceidratio":
		return sdktrace.TraceIDRatioBased(ratio(ctx, log))
	}
	log.WarnContext(ctx, "otel: unsupported sampler, using parentbased_always_on",
		"sampler", name)
	return sdktrace.ParentBased(sdktrace.AlwaysSample())
}

// ratio reads OTEL_TRACES_SAMPLER_ARG. Unset → 1.0; a value that is not a
// number in [0, 1] is logged and falls back to 1.0, as in the SDK.
func ratio(ctx context.Context, log *slog.Logger) float64 {
	arg := strings.TrimSpace(os.Getenv(envSamplerArg))
	if arg == "" {
		return 1
	}
	v, err := strconv.ParseFloat(arg, 64)
	// The success path comes first: written as a range check, the condition
	// also rejects NaN.
	if err == nil && v >= 0 && v <= 1 {
		return v
	}
	log.WarnContext(ctx, "otel: invalid sampler argument, using 1.0", "arg", arg)
	return 1
}

// suppressible drops spans started from a context marked by otel.Suppress and
// leaves every other decision to next.
type suppressible struct {
	next sdktrace.Sampler
}

// ShouldSample implements sdktrace.Sampler. A dropped span keeps the parent's
// tracestate: declining to record our own span must not destroy vendor state a
// downstream hop may still need.
func (s suppressible) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	if !skotel.IsSuppressed(p.ParentContext) {
		return s.next.ShouldSample(p)
	}
	return sdktrace.SamplingResult{
		Decision:   sdktrace.Drop,
		Tracestate: trace.SpanContextFromContext(p.ParentContext).TraceState(),
	}
}

// Description implements sdktrace.Sampler.
func (s suppressible) Description() string {
	return "Suppressible{" + s.next.Description() + "}"
}
