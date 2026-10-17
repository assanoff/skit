package setup

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	skotel "github.com/assanoff/skit/otel"
)

const defaultShutdownTimeout = 5 * time.Second

// Config configures the telemetry of a process.
type Config struct {
	// Service is service.name. Empty → OTEL_SERVICE_NAME, then the SDK's
	// unknown_service:<executable>.
	Service string
	// Version is service.version.
	Version string
	// Namespace is service.namespace.
	Namespace string
	// Environment is deployment.environment.name.
	Environment string
	// Sampler decides which spans are recorded; Suppress applies on top of it.
	// nil → OTEL_TRACES_SAMPLER and OTEL_TRACES_SAMPLER_ARG. Tests set it.
	Sampler sdktrace.Sampler
	// Exporter receives the sampled spans: otlp.NewExporter in a process,
	// tracetest in tests. nil → no span is sampled or exported; trace ids are
	// still created and propagated.
	Exporter sdktrace.SpanExporter
	// Global installs the provider, the propagator and the OTel error handler
	// and logger process-wide: true in a process, false in tests.
	Global bool
	// ShutdownTimeout bounds Shutdown; 0 → 5s.
	ShutdownTimeout time.Duration
}

// Telemetry is the OpenTelemetry SDK of a process.
type Telemetry struct {
	tp      *sdktrace.TracerProvider
	res     *resource.Resource
	timeout time.Duration
}

// New builds the Resource and the SDK TracerProvider. log receives the OTel
// diagnostics, never above WARN: a failing collector must not wake the ERROR
// hooks.
func New(ctx context.Context, log *slog.Logger, cfg Config) (*Telemetry, error) {
	if cfg.Global {
		otel.SetErrorHandler(errorHandler{log: log})
		otel.SetLogger(logr.FromSlogHandler(diagHandler{next: log.Handler()}))
	}

	res, err := newResource(ctx, log, cfg)
	if err != nil {
		return nil, err
	}
	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler(ctx, log, cfg)),
	}
	if cfg.Exporter != nil {
		opts = append(opts, sdktrace.WithBatcher(cfg.Exporter))
	}
	tp := sdktrace.NewTracerProvider(opts...)

	if cfg.Global {
		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(skotel.Propagator())
	}
	return &Telemetry{
		tp:      tp,
		res:     res,
		timeout: cmp.Or(cfg.ShutdownTimeout, defaultShutdownTimeout),
	}, nil
}

// TracerProvider returns the SDK provider for the Config of components.
func (t *Telemetry) TracerProvider() trace.TracerProvider {
	return t.tp
}

// Resource returns the Resource the spans carry.
func (t *Telemetry) Resource() *resource.Resource {
	return t.res
}

// Shutdown flushes the pending spans and stops the exporter within
// Config.ShutdownTimeout. It does not take the caller's context: shutdown
// starts after a signal canceled it. It is a dim.CleanupFunc.
func (t *Telemetry) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()
	if err := t.tp.Shutdown(ctx); err != nil {
		return fmt.Errorf("setup: shutdown: %w", err)
	}
	return nil
}

// newResource describes the process. The Config fields come last, so they
// override the environment.
func newResource(ctx context.Context, log *slog.Logger, cfg Config) (*resource.Resource, error) {
	res, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithService(),
		resource.WithFromEnv(),
		resource.WithAttributes(serviceAttrs(cfg)...),
	)
	if errors.Is(err, resource.ErrPartialResource) || errors.Is(err, resource.ErrSchemaURLConflict) {
		log.WarnContext(ctx, "otel: resource", "err", err)
		return res, nil
	}
	if err != nil {
		return nil, fmt.Errorf("setup: resource: %w", err)
	}
	return res, nil
}

// serviceAttrs returns the non-empty identity fields of cfg.
func serviceAttrs(cfg Config) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 4)
	for _, a := range []attribute.KeyValue{
		semconv.ServiceName(cfg.Service),
		semconv.ServiceVersion(cfg.Version),
		semconv.ServiceNamespace(cfg.Namespace),
		semconv.DeploymentEnvironmentNameKey.String(cfg.Environment),
	} {
		if a.Value.AsString() != "" {
			attrs = append(attrs, a)
		}
	}
	return attrs
}
