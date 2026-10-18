package setup

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/matryer/is"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// gauged returns the labels and the value of the only series of the gauge name
// on reg.
func gauged(t *testing.T, reg *prometheus.Registry, name string) (map[string]string, float64) {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		if len(f.GetMetric()) != 1 {
			t.Fatalf("%s: %d series, want 1", name, len(f.GetMetric()))
		}
		m := f.GetMetric()[0]
		labels := map[string]string{}
		for _, lp := range m.GetLabel() {
			labels[lp.GetName()] = lp.GetValue()
		}
		return labels, m.GetGauge().GetValue()
	}
	t.Fatalf("%s is not registered", name)
	return nil, 0
}

// target_info carries every attribute of the Resource, the environment's too.
func TestTargetInfo(t *testing.T) {
	is := is.New(t)
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "k8s.pod.name=pod-1,1st=x,_private=y")
	reg := prometheus.NewRegistry()

	newTelemetry(t.Context(), t, Config{
		Service: "orders", Version: "v1.2.3", Environment: "production", Metrics: reg,
	})

	labels, value := gauged(t, reg, "target_info")
	is.Equal(value, 1.0)
	is.Equal(labels["service_name"], "orders")
	is.Equal(labels["service_version"], "v1.2.3")
	is.Equal(labels["deployment_environment_name"], "production")
	is.Equal(labels["k8s_pod_name"], "pod-1")
	is.Equal(labels["key_1st"], "x")
	is.Equal(labels["key_private"], "y")
	is.Equal(labels["telemetry_sdk_language"], "go")
	is.True(labels["service_instance_id"] != "") // the Resource's own attributes too
	is.True(labels["host_name"] != "")
}

func TestTargetLabelsJoinCollisions(t *testing.T) {
	is := is.New(t)
	res := resource.NewSchemaless(attribute.String("a.b", "1"), attribute.String("a_b", "2"))

	is.Equal(targetLabels(res), prometheus.Labels{"a_b": "1;2"}) // values in key order
}

func TestLabelName(t *testing.T) {
	for _, tc := range []struct{ key, want string }{
		{"service.name", "service_name"},
		{"k8s.pod.name", "k8s_pod_name"},
		{"1st", "key_1st"},
		{"_private", "key_private"},
		{"__reserved", "key__reserved"},
		{"ключ", "key____"},
	} {
		if got := labelName(tc.key); got != tc.want {
			t.Errorf("labelName(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}
}

// flakyExporter fails every export while fail is set.
type flakyExporter struct {
	fail atomic.Bool
}

var errExport = errors.New("collector unavailable")

func (e *flakyExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	if e.fail.Load() {
		return errExport
	}
	return nil
}

func (e *flakyExporter) Shutdown(context.Context) error { return nil }

// Spans are counted by the result of the export they went out in.
func TestExportedSpans(t *testing.T) {
	is := is.New(t)
	reg := prometheus.NewRegistry()
	exp := &flakyExporter{}
	exp.fail.Store(true)
	tel := newTelemetry(t.Context(), t, Config{
		Sampler: sdktrace.AlwaysSample(), Exporter: exp, Metrics: reg,
	})
	tracer := tel.TracerProvider().Tracer("t")

	_, span := tracer.Start(t.Context(), "lost")
	span.End()
	is.True(errors.Is(tel.tp.ForceFlush(t.Context()), errExport))
	exp.fail.Store(false)
	for range 2 {
		_, span := tracer.Start(t.Context(), "sent")
		span.End()
	}
	is.NoErr(tel.tp.ForceFlush(t.Context()))

	want := `
# HELP skit_otel_exported_spans_total Spans passed to the exporter, by result: ok or error.
# TYPE skit_otel_exported_spans_total counter
skit_otel_exported_spans_total{result="error"} 1
skit_otel_exported_spans_total{result="ok"} 2
`
	is.NoErr(testutil.GatherAndCompare(reg, strings.NewReader(want), "skit_otel_exported_spans_total"))
}

// Without a registry the exporter goes to the batcher as it is.
func TestExporterWithoutMetrics(t *testing.T) {
	is := is.New(t)
	exp := tracetest.NewInMemoryExporter()

	is.Equal(exporter(Config{Exporter: exp}), sdktrace.SpanExporter(exp))
}
