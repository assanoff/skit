package setup

import (
	"context"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/assanoff/skit/internal/prom"
)

// setTargetInfo sets target_info to 1 on reg, labeled with every attribute of
// res: the info metric the OpenTelemetry Prometheus conventions describe the
// target with. service_name stays among the labels: job and instance come from
// the scrape config, not from the Resource, and service_name joins the
// metrics to the traces.
func setTargetInfo(reg prometheus.Registerer, res *resource.Resource) {
	prom.MustRegister(reg, prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "target_info",
		Help:        "Target metadata: the attributes of the OpenTelemetry Resource.",
		ConstLabels: targetLabels(res),
	})).Set(1)
}

// targetLabels turns the attributes of res into labels. Keys that map to one
// label name join their values with ";" in key order, as the conventions say.
func targetLabels(res *resource.Resource) prometheus.Labels {
	labels := prometheus.Labels{}
	for _, kv := range res.Attributes() {
		name := labelName(string(kv.Key))
		value := strings.ToValidUTF8(kv.Value.String(), "�")
		if prev, ok := labels[name]; ok {
			value = prev + ";" + value
		}
		labels[name] = value
	}
	return labels
}

// labelName maps an attribute key to a label name: every rune but an ASCII
// letter or digit becomes "_", a leading digit gets "key_" and a leading "_"
// gets "key", so an attribute from OTEL_RESOURCE_ATTRIBUTES never becomes a
// name Prometheus reserves or rejects.
func labelName(key string) string {
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '_'
	}, key)
	switch {
	case name[0] >= '0' && name[0] <= '9':
		return "key_" + name
	case name[0] == '_':
		return "key" + name
	default:
		return name
	}
}

// exporter returns cfg.Exporter, counted on cfg.Metrics when it is set.
func exporter(cfg Config) sdktrace.SpanExporter {
	if cfg.Metrics == nil {
		return cfg.Exporter
	}
	return newCountingExporter(cfg.Metrics, cfg.Exporter)
}

// countingExporter counts the spans it passes to the embedded exporter by
// result; Shutdown goes to the exporter as it is.
type countingExporter struct {
	sdktrace.SpanExporter

	ok     prometheus.Counter
	failed prometheus.Counter
}

// newCountingExporter builds the counters of skit_otel_exported_spans_total on
// reg; both results start at 0.
func newCountingExporter(reg prometheus.Registerer, next sdktrace.SpanExporter) *countingExporter {
	spans := prom.MustRegister(reg, prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "skit", Subsystem: "otel",
		Name: "exported_spans_total",
		Help: "Spans passed to the exporter, by result: ok or error.",
	}, []string{"result"}))
	return &countingExporter{
		SpanExporter: next,
		ok:           spans.WithLabelValues("ok"),
		failed:       spans.WithLabelValues("error"),
	}
}

// ExportSpans implements sdktrace.SpanExporter.
func (e *countingExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	err := e.SpanExporter.ExportSpans(ctx, spans)
	n := float64(len(spans))
	if err != nil {
		e.failed.Add(n)
		return err
	}
	e.ok.Add(n)
	return nil
}
