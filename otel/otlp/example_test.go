package otlp_test

import (
	"context"
	"log/slog"

	"github.com/assanoff/skit/otel/otlp"
	"github.com/assanoff/skit/otel/setup"
)

// A process exports its spans to the collector the OTEL_* variables name;
// OTEL_TRACES_EXPORTER=none leaves the exporter nil and setup exports
// nothing. The example needs a collector, so go test builds it but does not
// run it.
func Example() {
	ctx := context.Background()
	log := slog.Default()

	exp, err := otlp.NewExporter(ctx)
	if err != nil {
		log.ErrorContext(ctx, "exporter", "err", err)
		return
	}
	tel, err := setup.New(ctx, log, setup.Config{
		Service:  "orders",
		Exporter: exp,
		Global:   true,
	})
	if err != nil {
		log.ErrorContext(ctx, "telemetry", "err", err)
		return
	}
	defer func() { _ = tel.Shutdown() }() // closed last: flushes the spans

	// The servers and stores of the service take tel.TracerProvider() in
	// their Config.
	_, span := tel.TracerProvider().Tracer("example.com/orders").Start(ctx, "checkout")
	span.End()
}
