package debugsrv_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/assanoff/skit/buildinfo"
	"github.com/assanoff/skit/debugsrv"
	"github.com/assanoff/skit/health"
	"github.com/assanoff/skit/metrics"
	"github.com/assanoff/skit/worker"
)

// The status server listens on an internal address, apart from the public
// port, and joins the worker group of the REST and gRPC servers.
func Example() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	m := metrics.New()
	db := health.NamedChecker{Name: "db", Check: func(context.Context) error {
		return nil // e.g. pool.Ping
	}}

	srv := debugsrv.New(debugsrv.Config{
		Addr:           "localhost:6060",
		Logger:         log,
		MetricsHandler: m.Handler(),
		Liveness:       health.Liveness(),
		Readiness:      health.Readiness(time.Second, db),
		Version:        buildinfo.Read(),
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	grp := worker.NewGroup(log, worker.GroupConfig{ShutdownTimeout: 10 * time.Second})
	grp.Add(srv) // alongside the REST and gRPC servers
	if err := grp.Run(ctx); err != nil {
		log.ErrorContext(ctx, "run", "err", err)
	}
}

// Handler attaches the debug routes to the router of the application instead:
// one handler at each of Paths, outside the request timeout. A route whose
// handler Config leaves unset is not mounted.
func ExampleHandler() {
	dh := debugsrv.Handler(debugsrv.Config{
		Liveness: health.Liveness(),
		Version:  map[string]string{"version": "1.4.0"},
	})
	mux := http.NewServeMux() // the router of the application
	for _, p := range debugsrv.Paths {
		mux.Handle(p, dh)
	}

	for _, path := range []string{"/healthz", "/version", "/readyz", "/debug/pprof/cmdline"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		fmt.Println(path, w.Code)
	}
	// Output:
	// /healthz 200
	// /version 200
	// /readyz 404
	// /debug/pprof/cmdline 200
}
