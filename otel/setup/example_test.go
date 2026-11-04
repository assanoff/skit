package setup_test

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/assanoff/skit/otel/setup"
)

// keepSpans is the exporter in memory of the OTel SDK, but its spans outlive
// Shutdown, which flushes them.
type keepSpans struct {
	*tracetest.InMemoryExporter
}

func (keepSpans) Shutdown(context.Context) error { return nil }

// New installs the SDK: the provider sends the sampled spans to the Exporter,
// each with the Resource of the process, and Shutdown flushes them. A process
// takes the exporter from otel/otlp and sets Global; an exporter in memory
// stands in for it here.
func Example() {
	exp := keepSpans{tracetest.NewInMemoryExporter()}
	tel, err := setup.New(context.Background(), slog.New(slog.DiscardHandler), setup.Config{
		Service:     "orders",
		Version:     "1.4.0",
		Environment: "staging",
		Exporter:    exp,
		Sampler:     sdktrace.AlwaysSample(),
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	_, span := tel.TracerProvider().Tracer("example.com/orders").Start(context.Background(), "checkout")
	span.End()
	fmt.Println("shutdown:", tel.Shutdown())

	for _, s := range exp.GetSpans() {
		fmt.Println(s.Name, s.SpanContext.IsSampled())
		for _, key := range []string{"service.name", "service.version", "deployment.environment.name"} {
			v, _ := s.Resource.Set().Value(attribute.Key(key))
			fmt.Println(key, v.String())
		}
	}
	// Output:
	// shutdown: <nil>
	// checkout true
	// service.name orders
	// service.version 1.4.0
	// deployment.environment.name staging
}

// With a registry, target_info names the process and
// skit_otel_exported_spans_total counts the spans passed to the exporter.
func Example_metrics() {
	reg := prometheus.NewRegistry()
	tel, err := setup.New(context.Background(), slog.New(slog.DiscardHandler), setup.Config{
		Service:  "orders",
		Exporter: keepSpans{tracetest.NewInMemoryExporter()},
		Sampler:  sdktrace.AlwaysSample(),
		Metrics:  reg,
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, name := range []string{"checkout", "pay"} {
		_, span := tel.TracerProvider().Tracer("example.com/orders").Start(context.Background(), name)
		span.End()
	}
	if err := tel.Shutdown(); err != nil {
		fmt.Println(err)
		return
	}

	mfs, err := reg.Gather()
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, mf := range mfs {
		for _, m := range mf.GetMetric() {
			switch mf.GetName() {
			case "skit_otel_exported_spans_total":
				fmt.Println(mf.GetName(), "result="+label(m, "result"), m.GetCounter().GetValue())
			case "target_info":
				fmt.Println(mf.GetName(), "service_name="+label(m, "service_name"), m.GetGauge().GetValue())
			}
		}
	}
	// Output:
	// skit_otel_exported_spans_total result=error 0
	// skit_otel_exported_spans_total result=ok 2
	// target_info service_name=orders 1
}

// label returns the value of the label name of m, or "".
func label(m *dto.Metric, name string) string {
	for _, l := range m.GetLabel() {
		if l.GetName() == name {
			return l.GetValue()
		}
	}
	return ""
}
