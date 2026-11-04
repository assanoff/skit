package otel_test

import (
	"context"
	"fmt"
	"log/slog"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/otel"
	"github.com/assanoff/skit/otel/setup"
)

// keepSpans is the exporter in memory of the OTel SDK, but its spans outlive
// Shutdown, which flushes them.
type keepSpans struct {
	*tracetest.InMemoryExporter
}

func (keepSpans) Shutdown(context.Context) error { return nil }

// newTelemetry installs the SDK as a process does, with an exporter in memory
// in place of OTLP.
func newTelemetry(exp sdktrace.SpanExporter) (*setup.Telemetry, error) {
	return setup.New(context.Background(), slog.New(slog.DiscardHandler), setup.Config{
		Service:  "orders",
		Exporter: exp,
		Sampler:  sdktrace.AlwaysSample(),
	})
}

// A component takes its tracer once, from the provider of its Config, with its
// import path as the scope. SetError marks a failed span: status Error and
// error.type, here the code of an *errs.Error.
func Example() {
	exp := keepSpans{tracetest.NewInMemoryExporter()}
	tel, err := newTelemetry(exp)
	if err != nil {
		fmt.Println(err)
		return
	}
	tracer := otel.Tracer(tel.TracerProvider(), "example.com/orders/store")

	_, span := tracer.Start(context.Background(), "load order")
	otel.SetError(span, errs.Newf(errs.NotFound, "order 42 not found"))
	span.End()

	if err := tel.Shutdown(); err != nil { // flushes the spans
		fmt.Println(err)
		return
	}
	for _, s := range exp.GetSpans() {
		fmt.Println(s.Name, s.InstrumentationScope.Name)
		fmt.Println(s.Status.Code) // the error text is the Description
		for _, a := range s.Attributes {
			fmt.Println(a.Key, a.Value.String())
		}
	}
	// Output:
	// load order example.com/orders/store
	// Error
	// error.type not_found
}

// A background tick runs under Suppress, so the sampler of otel/setup drops
// its spans; the work the tick finds is traced after Unsuppress.
func ExampleSuppress() {
	exp := keepSpans{tracetest.NewInMemoryExporter()}
	tel, err := newTelemetry(exp)
	if err != nil {
		fmt.Println(err)
		return
	}
	tracer := otel.Tracer(tel.TracerProvider(), "example.com/orders/poller")

	tick := otel.Suppress(context.Background())
	_, poll := tracer.Start(tick, "claim jobs")
	poll.End()
	_, job := tracer.Start(otel.Unsuppress(tick), "send receipt")
	job.End()

	if err := tel.Shutdown(); err != nil {
		fmt.Println(err)
		return
	}
	for _, s := range exp.GetSpans() {
		fmt.Println(s.Name)
	}
	fmt.Println(otel.IsSuppressed(tick))
	// Output:
	// send receipt
	// true
}

// Carrier puts the trace context on a message, such as the headers of an
// outbox event; the consumer restores it with ExtractFromCarrier and its
// spans continue the trace.
func ExampleCarrier() {
	traceID, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	if err != nil {
		fmt.Println(err)
		return
	}
	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	if err != nil {
		fmt.Println(err)
		return
	}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(
		trace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled}))

	headers := otel.Carrier(ctx)
	fmt.Println(headers)

	sc := trace.SpanContextFromContext(otel.ExtractFromCarrier(context.Background(), headers))
	fmt.Println(sc.TraceID(), sc.SpanID(), sc.IsRemote())
	// Output:
	// map[traceparent:00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01]
	// 4bf92f3577b34da6a3ce929d0e0e4736 00f067aa0ba902b7 true
}
