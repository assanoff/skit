// Package grpcserver bootstraps a gRPC server with the SDK's cross-cutting
// concerns and exposes it as a worker.Runnable.
//
// It applies the same chain of unary interceptors as the REST stack — panic
// recovery, trace-id injection (extracting inbound W3C trace context from gRPC
// metadata), structured access logging, Prometheus metrics, and errs->status
// mapping — then registers a health service and, optionally, server reflection.
// Because Server implements worker.Runnable (Name/Start/Stop), it is supervised
// alongside the HTTP server and background workers.
//
// Interceptor order is recovery (outermost) -> trace -> logging -> metrics ->
// errormap -> application interceptors -> handler, so logging and metrics
// observe the already-mapped status code.
//
// # Usage
//
//	srv := grpcserver.New(log, grpcserver.Config{
//	    Addr:             ":9090",
//	    EnableReflection: true,
//	},
//	    grpcserver.WithTracer(tracer),
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
//   - MaxRecvMsgSize / MaxSendMsgSize: override the 4 MiB defaults.
//   - NumStreamWorkers / SharedWriteBuffer: stream-serving tuning.
//   - Keepalive (KeepaliveConfig): server keepalive parameters and enforcement
//     policy; zero values fall back to gRPC defaults.
//
// # Options
//
//   - WithTracer(t): tracer used for trace-id injection (defaults to a no-op).
//   - WithMetrics(reg): registers gRPC request count/latency collectors on reg
//     (typically the shared metrics.Metrics.Registry).
//   - WithUnaryInterceptors(in...): appends application interceptors, applied
//     after the built-in ones (closest to the handler).
package grpcserver
