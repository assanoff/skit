// Package setup installs the OpenTelemetry SDK of a process: the
// TracerProvider, the Resource, the sampler, the propagator and the OTel
// diagnostics. It does not import gRPC; the OTLP exporter comes from
// otel/otlp. SDK packages never import setup: they take a
// trace.TracerProvider in their Config.
//
// # Usage
//
// Build it first among the dependencies, so it is closed last and the spans of
// the stopping servers are flushed:
//
//	exp, err := otlp.NewExporter(ctx) // nil when OTEL_TRACES_EXPORTER=none
//	if err != nil {
//		return err
//	}
//	tel, err := setup.New(ctx, log, setup.Config{
//		Service:     "orders",
//		Version:     version,
//		Environment: "production",
//		Exporter:    exp,
//		Metrics:     reg,
//		Global:      true,
//	})
//	if err != nil {
//		return err
//	}
//	defer tel.Shutdown()
//	srv := grpcserver.New(log, grpcserver.Config{TracerProvider: tel.TracerProvider()})
//
// # Trace context without export
//
// The SDK provider is installed even when nothing is exported. With a nil
// Exporter no span is sampled, but every root still gets a W3C trace id: log
// records carry it and outgoing calls propagate it with trace flags 00.
//
// # Sampling
//
// OTEL_TRACES_SAMPLER and OTEL_TRACES_SAMPLER_ARG choose the sampler with the
// values and defaults of the OpenTelemetry specification: always_on,
// always_off, traceidratio and their parentbased_ forms; unset means
// parentbased_always_on, and the ratio defaults to 1.0. setup reads them
// itself, because the Suppress wrapper replaces the SDK's own reading. An
// unsupported value is logged at WARN and the default is used.
//
// otel.Suppress applies on top of any sampler: a span started from a
// suppressed context is dropped, and otel.Unsuppress lifts the mark.
//
// A fleet keeps one ratio for all services: with traceidratio a service samples
// independently of its callers, and equal ratios keep traces whole between Go
// services.
//
// # Resource
//
// The Resource holds telemetry.sdk.*, host.name, service.instance.id, the
// attributes from OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES, and the
// non-empty Config fields, which win over the environment: service.name,
// service.version, service.namespace and deployment.environment.name — the
// values the log records carry.
//
// # Diagnostics
//
// With Global, OTel's errors (a failed export, an invalid attribute) and its
// internal logger write to the given *slog.Logger: errors and warnings at WARN,
// the rest at DEBUG. Nothing goes to stderr as plain text, and a failing
// collector never reaches ERROR.
//
// # Metrics
//
// With a registry in Config.Metrics, New sets target_info to 1, labeled with
// every attribute of the Resource under its Prometheus name (service.name →
// service_name, k8s.pod.name → k8s_pod_name): a dashboard sees which service,
// version and environment run in each target and joins the metrics to the
// traces by service_name. It also counts the spans passed to the Exporter in
// skit_otel_exported_spans_total by result (ok, error); without an Exporter
// the counter does not exist. Spans the batcher drops on a full queue never
// reach the exporter and are not counted; they pile up when exports fail, which
// result="error" shows. Build the registry before setup, so the process
// exposes these next to its own metrics.
//
// # Config
//
//   - Service, Version, Namespace, Environment: the identity of the process;
//     empty fields are not written.
//   - Sampler: overrides the sampler from the environment; tests set it.
//   - Exporter: otlp.NewExporter in a process, tracetest in tests; nil → no
//     export.
//   - Metrics: the registry for target_info and skit_otel_*; nil → no
//     metrics. Tests leave it nil.
//   - Global: install the provider, the propagator (W3C TraceContext only) and
//     the diagnostics process-wide; true in a process, false in tests.
//   - ShutdownTimeout: bound for Shutdown; 0 → 5s.
package setup
