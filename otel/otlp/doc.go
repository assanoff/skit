// Package otlp builds the OTLP/gRPC span exporter for otel/setup. It is the
// only part of the telemetry setup that pulls in gRPC: a binary that never
// exports does not import it.
//
// # Usage
//
//	exp, err := otlp.NewExporter(ctx) // nil when OTEL_TRACES_EXPORTER=none
//	if err != nil {
//		return err
//	}
//	tel, err := setup.New(ctx, log, setup.Config{Service: "orders", Exporter: exp})
//
// # Environment
//
// The variables follow the OpenTelemetry specification:
//   - OTEL_TRACES_EXPORTER: otlp (the default) or none; any other value is an
//     error.
//   - OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_EXPORTER_OTLP_TRACES_ENDPOINT: the
//     collector, by default localhost:4317. An http:// scheme disables TLS.
//   - OTEL_EXPORTER_OTLP_HEADERS, _TIMEOUT, _COMPRESSION, _CERTIFICATE and their
//     _TRACES_ variants: read by the exporter as is.
//   - OTEL_EXPORTER_OTLP_PROTOCOL, OTEL_EXPORTER_OTLP_TRACES_PROTOCOL: only
//     grpc is supported; another value is an error. Unset means grpc, while the
//     specification defaults to http/protobuf.
package otlp
