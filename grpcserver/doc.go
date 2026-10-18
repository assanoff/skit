// Package grpcserver bootstraps a gRPC server with the SDK's cross-cutting
// concerns and exposes it as a worker.Runnable.
//
// It opens a server span per RPC (otelgrpc), then applies the same chain of
// unary interceptors as the REST stack — Prometheus metrics, panic recovery,
// structured access logging, and errs->status mapping — and registers a
// health service and, optionally, server reflection. Because Server implements
// worker.Runnable (Name/Start/Stop), it is supervised alongside the HTTP server
// and background workers.
//
// Interceptor order is metrics (outermost) -> recovery -> logging -> errormap
// -> application interceptors -> handler, so metrics and logging observe the
// already-mapped status code, and metrics count a panic as Internal.
//
// # Tracing
//
// The server span is opened before the interceptors, so every interceptor and
// the handler run inside it and their log records carry its trace id. It
// continues the W3C trace context of the request metadata. Health checks,
// reflection and methods the server does not serve get no span: a scanner
// cannot fill the trace UI with spans of made-up names.
//
// # Metrics
//
// With Config.Metrics set, the server records on that registry:
//   - skit_grpc_server_handled_total by method and code;
//   - skit_grpc_server_handling_seconds by method;
//   - skit_grpc_server_in_flight.
//
// The method is recorded without its leading "/" ("pkg.Service/Method"), the
// code by its canonical name ("NOT_FOUND"), and the duration of a sampled RPC
// carries its trace as the exemplar. Like spans, health checks and reflection
// are not recorded.
//
// # Logging
//
// Every RPC is one info record, "grpc.request" ("grpc.stream" for a stream),
// with the fields of the RPC semantic conventions and the values the metrics
// record: rpc.method without its leading "/", rpc.response.status_code by its
// canonical name and rpc.server.call.duration in seconds.
//
// # Usage
//
//	srv := grpcserver.New(log, grpcserver.Config{
//	    Addr:             ":9090",
//	    EnableReflection: true,
//	    Metrics:          d.Metrics,
//	    TracerProvider:   tp,
//	})
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
//   - Metrics: the registry of the RPC metrics; nil disables them.
//   - TracerProvider: records the server spans; nil → the global provider.
//   - MaxRecvMsgSize / MaxSendMsgSize: override the 4 MiB defaults.
//   - NumStreamWorkers: stream-serving tuning (shared write buffers are
//     gRPC's default since v1.84).
//   - Keepalive (KeepaliveConfig): server keepalive parameters and enforcement
//     policy; zero values fall back to gRPC defaults.
//
// # Options
//
//   - WithUnaryInterceptors(in...): appends application interceptors, applied
//     after the built-in ones (closest to the handler).
package grpcserver
