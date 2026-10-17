package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/matryer/is"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	skotel "github.com/assanoff/skit/otel"
)

// jsonLog returns a logger writing JSON lines at DEBUG into buf.
func jsonLog(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// records decodes the JSON lines in buf.
func records(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for ln := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		if ln == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			t.Fatalf("decode %q: %v", ln, err)
		}
		out = append(out, m)
	}
	return out
}

func newTelemetry(ctx context.Context, t *testing.T, cfg Config) *Telemetry {
	t.Helper()
	tel, err := New(ctx, slog.New(slog.DiscardHandler), cfg)
	if err != nil {
		t.Fatalf("setup.New: %v", err)
	}
	t.Cleanup(func() { _ = tel.Shutdown() })
	return tel
}

func TestResource(t *testing.T) {
	is := is.New(t)
	t.Setenv("OTEL_SERVICE_NAME", "from-env")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "k8s.pod.name=pod-1")
	exp := tracetest.NewInMemoryExporter()
	var buf bytes.Buffer
	tel, err := New(t.Context(), jsonLog(&buf), Config{
		Service: "orders", Version: "v1.2.3", Environment: "production",
		Sampler: sdktrace.AlwaysSample(), Exporter: exp,
	})
	is.NoErr(err)
	defer func() { _ = tel.Shutdown() }()
	is.Equal(buf.Len(), 0) // no partial resource, no schema URL conflict

	_, span := tel.TracerProvider().Tracer("t").Start(t.Context(), "op")
	span.End()
	is.NoErr(tel.tp.ForceFlush(t.Context()))             // Shutdown would reset the exporter
	is.Equal(exp.GetSpans()[0].Resource, tel.Resource()) // spans carry the Resource

	attrs := map[attribute.Key]string{}
	for _, kv := range tel.Resource().Attributes() {
		attrs[kv.Key] = kv.Value.String()
	}
	is.Equal(attrs["service.name"], "orders") // Config wins over OTEL_SERVICE_NAME
	is.Equal(attrs["service.version"], "v1.2.3")
	is.Equal(attrs["deployment.environment.name"], "production")
	is.Equal(attrs["k8s.pod.name"], "pod-1") // OTEL_RESOURCE_ATTRIBUTES
	is.Equal(attrs["telemetry.sdk.language"], "go")
	is.True(attrs["telemetry.sdk.version"] != "")
	is.True(attrs["service.instance.id"] != "")
	_, ok := attrs["service.namespace"]
	is.True(!ok) // empty Config field is not written
}

// Without an exporter nothing is sampled, yet every root has a trace id.
func TestNoExporterKeepsTraceContext(t *testing.T) {
	is := is.New(t)
	tel := newTelemetry(t.Context(), t, Config{Sampler: sdktrace.AlwaysSample()})

	_, span := tel.TracerProvider().Tracer("t").Start(t.Context(), "op")
	defer span.End()
	sc := span.SpanContext()
	is.True(sc.IsValid())
	is.Equal(sc.TraceFlags(), trace.TraceFlags(0)) // "00": downstream does not record it
	is.True(!span.IsRecording())
}

func TestSuppress(t *testing.T) {
	is := is.New(t)
	tracer := newTelemetry(t.Context(), t, Config{
		Sampler:  sdktrace.AlwaysSample(),
		Exporter: tracetest.NewInMemoryExporter(),
	}).TracerProvider().Tracer("t")

	tick := skotel.Suppress(t.Context())
	_, dropped := tracer.Start(tick, "poll")
	defer dropped.End()
	_, kept := tracer.Start(skotel.Unsuppress(tick), "dispatch")
	defer kept.End()

	is.True(!dropped.SpanContext().IsSampled())
	is.True(dropped.SpanContext().IsValid()) // still has a trace id
	is.True(kept.SpanContext().IsSampled())
}

// A dropped suppressed span keeps the parent's tracestate for the next hop.
func TestSuppressKeepsTracestate(t *testing.T) {
	is := is.New(t)
	ts, err := trace.ParseTraceState("vendor=state")
	is.NoErr(err)
	parent := trace.ContextWithSpanContext(t.Context(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1},
		SpanID:     trace.SpanID{2},
		TraceFlags: trace.FlagsSampled,
		TraceState: ts,
	}))
	s := suppressible{next: sdktrace.AlwaysSample()}

	res := s.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: skotel.Suppress(parent),
		Name:          "poll",
	})

	is.Equal(res.Decision, sdktrace.Drop)
	is.Equal(res.Tracestate.Get("vendor"), "state")
}

func TestSamplerFromEnv(t *testing.T) {
	tests := []struct {
		name, sampler, arg string
		want               sdktrace.Sampler
		warn               bool
	}{
		{name: "unset", want: sdktrace.ParentBased(sdktrace.AlwaysSample())},
		{name: "always_off", sampler: "always_off", want: sdktrace.NeverSample()},
		{
			name: "ratio", sampler: "TraceIdRatio", arg: " 0.25 ",
			want: sdktrace.TraceIDRatioBased(0.25),
		},
		{name: "ratio default", sampler: "traceidratio", want: sdktrace.TraceIDRatioBased(1)},
		{
			name: "parent ratio", sampler: "parentbased_traceidratio", arg: "0",
			want: sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0)),
		},
		{
			name: "parent off", sampler: "parentbased_always_off",
			want: sdktrace.ParentBased(sdktrace.NeverSample()),
		},
		{
			name: "bad arg", sampler: "traceidratio", arg: "abc",
			want: sdktrace.TraceIDRatioBased(1), warn: true,
		},
		{
			name: "NaN arg", sampler: "traceidratio", arg: "NaN",
			want: sdktrace.TraceIDRatioBased(1), warn: true,
		},
		{
			name: "arg above 1", sampler: "traceidratio", arg: "1.5",
			want: sdktrace.TraceIDRatioBased(1), warn: true,
		},
		{
			name: "unsupported", sampler: "jaeger_remote",
			want: sdktrace.ParentBased(sdktrace.AlwaysSample()), warn: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)
			t.Setenv(envSampler, tt.sampler)
			t.Setenv(envSamplerArg, tt.arg)
			var buf bytes.Buffer

			got := samplerFromEnv(t.Context(), jsonLog(&buf))

			is.Equal(got.Description(), tt.want.Description())
			recs := records(t, &buf)
			if !tt.warn {
				is.Equal(len(recs), 0)
				return
			}
			is.Equal(len(recs), 1) // one JSON line, nothing on stderr
			is.Equal(recs[0]["level"], "WARN")
		})
	}
}

// The sampler from the environment is used when Config.Sampler is nil, under
// Suppress.
func TestEnvSamplerUnderSuppress(t *testing.T) {
	is := is.New(t)
	t.Setenv(envSampler, "always_off")
	tel := newTelemetry(t.Context(), t, Config{Exporter: tracetest.NewInMemoryExporter()})

	_, span := tel.TracerProvider().Tracer("t").Start(t.Context(), "op")
	defer span.End()
	is.True(!span.SpanContext().IsSampled())
}

// blockingExporter never finishes an export, like a collector that hangs.
type blockingExporter struct {
	release chan struct{}
	once    sync.Once
}

func (e *blockingExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	<-e.release
	return nil
}

func (e *blockingExporter) Shutdown(context.Context) error {
	<-e.release
	return nil
}

func (e *blockingExporter) unblock() { e.once.Do(func() { close(e.release) }) }

func TestShutdownBoundedByTimeout(t *testing.T) {
	is := is.New(t)
	exp := &blockingExporter{release: make(chan struct{})}
	defer exp.unblock()
	tel, err := New(t.Context(), slog.New(slog.DiscardHandler), Config{
		Sampler:         sdktrace.AlwaysSample(),
		Exporter:        exp,
		ShutdownTimeout: 50 * time.Millisecond,
	})
	is.NoErr(err)
	_, span := tel.TracerProvider().Tracer("t").Start(t.Context(), "op")
	span.End()

	start := time.Now()
	err = tel.Shutdown()
	is.True(errors.Is(err, context.DeadlineExceeded))
	is.True(time.Since(start) < time.Second)
}

func TestErrorHandlerWarns(t *testing.T) {
	is := is.New(t)
	var buf bytes.Buffer

	errorHandler{log: jsonLog(&buf)}.Handle(errors.New("export failed"))

	recs := records(t, &buf)
	is.Equal(len(recs), 1)
	is.Equal(recs[0]["level"], "WARN")
	is.Equal(recs[0]["err"], "export failed")
}

// OTel's internal logger reaches slog at WARN for errors and warnings and at
// DEBUG for the rest.
func TestDiagnosticLevels(t *testing.T) {
	is := is.New(t)
	var buf bytes.Buffer
	l := logr.FromSlogHandler(diagHandler{next: jsonLog(&buf).Handler()})

	l.Error(errors.New("boom"), "error")
	l.V(1).Info("warn")
	l.V(4).Info("info")
	l.V(8).Info("debug")

	recs := records(t, &buf)
	is.Equal(len(recs), 4)
	is.Equal(recs[0]["level"], "WARN")
	is.Equal(recs[0]["err"], "boom")
	is.Equal(recs[1]["level"], "WARN")
	is.Equal(recs[2]["level"], "DEBUG")
	is.Equal(recs[3]["level"], "DEBUG")
}

func TestDiagnosticsRespectLevel(t *testing.T) {
	is := is.New(t)
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	l := logr.FromSlogHandler(diagHandler{next: h})

	is.True(!l.V(4).Enabled()) // DEBUG is off at INFO
	l.V(4).Info("info")
	l.V(1).Info("warn")

	recs := records(t, &buf)
	is.Equal(len(recs), 1)
	is.Equal(recs[0]["msg"], "warn")
}
