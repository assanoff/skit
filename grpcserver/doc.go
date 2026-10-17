// Package grpcserver bootstraps a gRPC server with the SDK's cross-cutting
// concerns and exposes it as a worker.Runnable.
//
// It opens a server span per RPC (otelgrpc), then applies the same chain of
// unary interceptors as the REST stack — panic recovery, structured access
// logging, Prometheus metrics, and errs->status mapping — and registers a
// health service and, optionally, server reflection. Because Server implements
// worker.Runnable (Name/Start/Stop), it is supervised alongside the HTTP server
// and background workers.
//
// Interceptor order is recovery (outermost) -> logging -> metrics -> errormap
// -> application interceptors -> handler, so logging and metrics observe the
// already-mapped status code.
//
// # Tracing
//
// The server span is opened before the interceptors, so every interceptor and
// the handler run inside it and their log records carry its trace id. It
// continues the W3C trace context of the request metadata. Health checks,
// reflection and methods the server does not serve get no span: a scanner
// cannot fill the trace UI with spans of made-up names.
//
// # Usage
//
//	srv := grpcserver.New(log, grpcserver.Config{
//	    Addr:             ":9090",
//	    EnableReflection: true,
//	    TracerProvider:   tp,
//	},
//	    grpcserver.WithMetrics(m.Registry),
//	)
//	srv.Install(handler) // each feature's Register owns its RegisterXxxServer call
//	group.Add(srv)       // worker.Runnable: Start binds the listener, Stop drains
//
// In tests, Serve runs the server on a caller-provided listener (e.g. a bufconn
// listener) instead of binding a TCP port.
//
// # Config
//
//   - Addr: listen address, e.g. ":9090".
//   - ShutdownTimeout: bounds graceful drain before a hard stop (default 10s).
//   - EnableReflection: turns on server reflection (handy for grpcurl in dev).
//   - MetricsNamespace: prefixes the gRPC metric names (default "grpc").
//   - TracerProvider: records the server spans; nil → the global provider.
//   - MaxRecvMsgSize / MaxSendMsgSize: override the 4 MiB defaults.
//   - NumStreamWorkers: stream-serving tuning (shared write buffers are
//     gRPC's default since v1.84).
//   - Keepalive (KeepaliveConfig): server keepalive parameters and enforcement
//     policy; zero values fall back to gRPC defaults.
//
// # Options
//
//   - WithMetrics(reg): registers gRPC request count/latency collectors on reg
//     (typically the shared metrics.Metrics.Registry).
//   - WithUnaryInterceptors(in...): appends application interceptors, applied
//     after the built-in ones (closest to the handler).
package grpcserver
