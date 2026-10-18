package otlp

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"strings"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// NewExporter returns the span exporter OTEL_TRACES_EXPORTER selects: otlp
// (the default) builds an OTLP/gRPC exporter, none returns a nil exporter and
// no error, which setup.Config.Exporter reads as "do not export". Any other
// value, and an OTLP protocol other than grpc, is an error.
//
// The exporter reads its endpoint, headers, TLS, timeout and compression from
// OTEL_EXPORTER_OTLP_* itself; an http:// endpoint disables TLS. It connects
// lazily, so an unreachable collector does not fail the start.
func NewExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	switch name := envValue("OTEL_TRACES_EXPORTER"); name {
	case "", "otlp":
		// The default: the exporter is built after the switch.
	case "none":
		return nil, nil //nolint:nilnil // a nil exporter is setup.Config's "do not export"
	default:
		return nil, fmt.Errorf("otlp: OTEL_TRACES_EXPORTER=%q: only otlp and none are supported", name)
	}

	protocol := cmp.Or(envValue("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL"),
		envValue("OTEL_EXPORTER_OTLP_PROTOCOL"))
	if protocol != "" && protocol != "grpc" {
		return nil, fmt.Errorf("otlp: OTLP protocol %q: only grpc is supported", protocol)
	}

	exp, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("otlp: create exporter: %w", err)
	}
	return exp, nil
}

func envValue(key string) string {
	return strings.ToLower(strings.TrimSpace(os.Getenv(key)))
}
