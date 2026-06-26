package provider

import (
	"context"

	"go.opentelemetry.io/otel/trace"

	"github.com/assanoff/skit/dim"
	"github.com/assanoff/skit/otel"
)

// Tracer returns a dim factory that bootstraps OTLP/gRPC tracing from cfg and
// returns the named Tracer; the cleanup flushes pending spans and shuts the
// exporter down.
//
// This always initializes the exporter. Gate it on your own "tracing enabled"
// flag — when disabled, assign a no-op tracer instead of calling this:
//
//	if !opts.OTEL.Enabled {
//		c.Tracer = func(context.Context) trace.Tracer {
//			return noop.NewTracerProvider().Tracer(opts.Service)
//		}
//		return nil, nil
//	}
//	c.Tracer, cleanup = dim.NewResource("Tracer", provider.Tracer(otel.Config{...}))
func Tracer(cfg otel.Config) func(ctx context.Context) (trace.Tracer, dim.CleanupFunc, error) {
	return func(ctx context.Context) (trace.Tracer, dim.CleanupFunc, error) {
		tracer, shutdown, err := otel.InitTracing(ctx, cfg)
		if err != nil {
			return nil, nil, err
		}
		return tracer, func() error { return shutdown(context.Background()) }, nil
	}
}
