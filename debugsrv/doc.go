// Package debugsrv serves operational endpoints — net/http/pprof profiles plus
// optional metrics and health handlers — on a separate, internal-only listener
// or as a handler attached to an existing router.
//
// Keeping pprof and metrics off the public application port avoids exposing
// profiling and internal signals to the internet; bind Addr to localhost or a
// cluster-internal interface and scrape it from your metrics/ops tooling. The
// endpoints live at several fixed top-level paths (Paths): /debug/pprof/,
// /metrics, /healthz, /readyz, /startupz, /version. The metrics, health,
// startup and version routes are only mounted when their handler (or value) is
// supplied in Config.
//
// debugsrv is the "status server" brick of the server kit: it builds on the
// shared httpserver core (the same http.Handler-on-a-listener Runnable that
// backs the REST and gRPC-gateway servers), so all of them supervise and drain
// together under one worker.Group.
//
// # Standalone server
//
// New returns a *Server implementing worker.Runnable (Start/Stop/Name), so it
// joins a supervisor alongside the REST/gRPC servers and shuts down with them:
//
//	grp.Add(debugsrv.New(debugsrv.Config{
//		Addr:           "localhost:6060",
//		MetricsHandler: m.Handler(),
//		Liveness:       health.Liveness(),
//		Readiness:      health.Readiness(time.Second, dbCheck),
//	}))
//
// # Attached to the application router
//
// Handler returns the routes as a single http.Handler. Because the endpoints
// live at several fixed top-level paths, register the one handler at each of
// them (Paths) rather than under a single prefix:
//
//	dh := debugsrv.Handler(debugsrv.Config{MetricsHandler: m.Handler()})
//	for _, p := range debugsrv.Paths {
//		appRouter.Handle(p, dh)
//	}
//
// *Server also implements http.Handler (ServeHTTP delegates to the same routes),
// so the same instance can run standalone via Start or be reused as a handler.
//
// Attach the debug routes WITHOUT the per-request timeout/size-limit
// middleware — pprof profile/trace handlers run for many seconds and a request
// timeout would cut them off.
//
// # Config
//
// Config fields (only Addr is required for the standalone server):
//   - Addr: listen address, e.g. "localhost:6060".
//   - ReadHeaderTimeout: bounds reading request headers (default 5s) to avoid
//     Slowloris-style stalls on the debug port.
//   - ShutdownTimeout: bounds graceful shutdown when Stop's ctx has no deadline
//     (default 10s).
//   - Logger: receives start/stop lines; defaults to slog.Default().
//   - MetricsHandler: when set, served at /metrics.
//   - Liveness: when set, served at /healthz.
//   - Readiness: when set, served at /readyz.
//   - Startup: when set, served at /startupz (Kubernetes startup probe).
//   - Version: when non-nil, encoded as JSON at /version (build/version info).
package debugsrv
