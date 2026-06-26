package debugsrv

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/assanoff/skit/httpserver"
)

// Config configures a debug Server. Only Addr is required.
type Config struct {
	// Addr is the listen address, e.g. "localhost:6060".
	Addr string
	// ReadHeaderTimeout bounds reading request headers (default 5s) to avoid
	// Slowloris-style stalls on the debug port.
	ReadHeaderTimeout time.Duration
	// ShutdownTimeout bounds graceful shutdown when Stop's context has no
	// deadline (default 10s).
	ShutdownTimeout time.Duration
	// Logger receives start/stop lines; defaults to slog.Default().
	Logger *slog.Logger
	// MetricsHandler, when set, is served at /metrics.
	MetricsHandler http.Handler
	// Liveness, when set, is served at /healthz.
	Liveness http.Handler
	// Readiness, when set, is served at /readyz.
	Readiness http.Handler
	// Startup, when set, is served at /startupz (Kubernetes startup probe). Pass
	// health.Liveness() for an always-OK probe, or a custom check.
	Startup http.Handler
	// Version, when non-nil, is encoded as JSON at /version — typically a small
	// struct or map of build info (name, version, commit, build date, env).
	Version any
}

// Server serves the debug endpoints on its own listener. It embeds an
// httpserver.Server (so it is a worker.Runnable via Start/Stop/Name) and also
// implements http.Handler, so it can run standalone or be reused as a handler.
type Server struct {
	*httpserver.Server
	handler http.Handler
}

// New builds a debug Server from cfg. The returned server is not listening
// until Start is called.
func New(cfg Config) *Server {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	h := Handler(cfg)
	srv := httpserver.New(httpserver.Config{
		Name:              "debug-server",
		Addr:              cfg.Addr,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ShutdownTimeout:   cfg.ShutdownTimeout,
		Logger:            log,
	}, h)
	return &Server{Server: srv, handler: h}
}

// ServeHTTP implements http.Handler, so the debug endpoints can be reused as a
// handler (mounted, tested, or wrapped) as well as run standalone via Start.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

// Paths are the top-level patterns Handler serves. An application that attaches
// the handler to its own router registers it at each of these.
var Paths = []string{"/debug/pprof/", "/metrics", "/healthz", "/readyz", "/startupz", "/version"}

// Handler builds the debug endpoints — pprof under /debug/pprof/ plus the
// optional metrics, health, startup and version endpoints from cfg — as a single
// http.Handler.
//
// This is the idiomatic primitive: serve it on a standalone listener (New wraps
// it) or attach it to an application router. Because the endpoints live at
// several fixed top-level paths (/metrics and /healthz by Prometheus/k8s
// convention, /debug/pprof/ with pprof's own links), attach the one handler at
// each of those patterns (Paths) rather than under a single prefix:
//
//	dh := debugsrv.Handler(cfg)
//	for _, p := range debugsrv.Paths {
//		appRouter.Handle(p, dh) // outside the per-request timeout middleware
//	}
func Handler(cfg Config) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	if cfg.MetricsHandler != nil {
		mux.Handle("/metrics", cfg.MetricsHandler)
	}
	if cfg.Liveness != nil {
		mux.Handle("/healthz", cfg.Liveness)
	}
	if cfg.Readiness != nil {
		mux.Handle("/readyz", cfg.Readiness)
	}
	if cfg.Startup != nil {
		mux.Handle("/startupz", cfg.Startup)
	}
	if cfg.Version != nil {
		mux.Handle("/version", versionHandler(cfg.Version))
	}
	return mux
}

// versionHandler serves v as JSON. Encoding errors are ignored: /version is a
// best-effort informational endpoint and v is caller-supplied static data.
func versionHandler(v any) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(v)
	}
}
