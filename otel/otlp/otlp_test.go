package otlp_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/matryer/is"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/assanoff/skit/otel/otlp"
	"github.com/assanoff/skit/otel/setup"
)

func TestNewExporterSelection(t *testing.T) {
	tests := []struct {
		name, exporter, protocol, tracesProtocol string
		wantExporter, wantErr                    bool
	}{
		{name: "default", wantExporter: true},
		{name: "otlp grpc", exporter: "OTLP", protocol: "grpc", wantExporter: true},
		{name: "none", exporter: "none"},
		{name: "unsupported exporter", exporter: "zipkin", wantErr: true},
		{name: "http protocol", protocol: "http/protobuf", wantErr: true},
		{
			name: "traces protocol wins", protocol: "http/protobuf",
			tracesProtocol: "grpc", wantExporter: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)
			t.Setenv("OTEL_TRACES_EXPORTER", tt.exporter)
			t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", tt.protocol)
			t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", tt.tracesProtocol)
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1")

			exp, err := otlp.NewExporter(t.Context())

			is.Equal(err != nil, tt.wantErr)
			is.Equal(exp != nil, tt.wantExporter)
			if exp != nil {
				is.NoErr(exp.Shutdown(t.Context()))
			}
		})
	}
}

// An unreachable collector neither fails the start nor holds up the shutdown.
func TestShutdownWithUnreachableCollector(t *testing.T) {
	is := is.New(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1")
	exp, err := otlp.NewExporter(t.Context())
	is.NoErr(err)
	tel, err := setup.New(t.Context(), slog.New(slog.DiscardHandler), setup.Config{
		Service:         "orders",
		Sampler:         sdktrace.AlwaysSample(),
		Exporter:        exp,
		ShutdownTimeout: 200 * time.Millisecond,
	})
	is.NoErr(err)
	_, span := tel.TracerProvider().Tracer("t").Start(t.Context(), "op")
	span.End()

	start := time.Now()
	_ = tel.Shutdown() // the export fails or times out; either way it returns
	is.True(time.Since(start) < 2*time.Second)
}
