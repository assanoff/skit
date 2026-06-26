// Package httpserver is the standard-library-idiomatic core of the server kit:
// a worker.Runnable that serves any http.Handler on its own listener with
// graceful shutdown.
//
// The seam is net/http's own abstraction, http.Handler — so the handler can be
// a stdlib *http.ServeMux, the SDK's rest/router, a grpc-gateway runtime.ServeMux,
// or any third-party router. REST, the gRPC-gateway and the status server
// (debugsrv) are all just this one brick wrapped around a different handler;
// register each with a single worker.Group and they start, fail and drain
// independently, like Lego.
//
// # Usage
//
//	srv := httpserver.New(httpserver.Config{
//		Name:            "rest-server",
//		Addr:            ":8080",
//		ShutdownTimeout: 10 * time.Second,
//		Logger:          log.Slog(),
//	}, router)
//
//	grp := worker.NewGroup(log.Slog(), 10*time.Second)
//	grp.Add(srv)          // alongside grpcserver.Server, debugsrv.Server, ...
//	_ = grp.Run(ctx)
//
// Tests and callers that manage their own listener can use Serve instead of
// Start:
//
//	lis, _ := net.Listen("tcp", "localhost:0")
//	go srv.Serve(lis)
//	// ... drive lis.Addr(); then srv.Stop(ctx)
//
// # Config
//
// Only Addr is required; everything else has a safe default.
//   - Addr: listen address, e.g. ":8080".
//   - Name: supervisor/log name (default "http-server"); set it so REST/gateway/
//     status read distinctly.
//   - ReadHeaderTimeout: bounds reading request headers (default 5s) against
//     Slowloris-style stalls; a negative value disables it.
//   - ReadTimeout / WriteTimeout / IdleTimeout: map to the http.Server fields of
//     the same name; zero leaves each at the net/http default.
//   - ShutdownTimeout: bounds graceful shutdown when Stop's ctx has no deadline
//     (default 10s).
//   - Logger: receives start/stop lines; defaults to slog.Default().
package httpserver
