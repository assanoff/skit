package grpcgateway_test

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/assanoff/skit/closer"
	"github.com/assanoff/skit/grpcgateway"
	"github.com/assanoff/skit/grpcserver"
	"github.com/assanoff/skit/httpserver"
	"github.com/assanoff/skit/middleware"
	"github.com/assanoff/skit/worker"
)

// The gateway runs as one more HTTP server of the service: it proxies to the
// gRPC server of the same process, and Close releases the connection it has
// dialed.
func Example() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The registrar protoc-gen-grpc-gateway generates for a service:
	// widgetv1.RegisterWidgetServiceHandler.
	var registerWidgets grpcgateway.HandlerRegistrar

	gw, err := grpcgateway.New(ctx, grpcgateway.Config{Endpoint: "localhost:9090"},
		registerWidgets)
	if err != nil {
		log.ErrorContext(ctx, "gateway", "err", err)
		return
	}
	closer.Add(gw.Close)

	grp := worker.NewGroup(log, worker.GroupConfig{ShutdownTimeout: 10 * time.Second})
	grp.Add(httpserver.New(httpserver.Config{
		Name: "gateway-server", Addr: ":8081", Logger: log,
	}, middleware.Trace(middleware.TraceConfig{})(gw)))
	if err := grp.Run(ctx); err != nil {
		log.ErrorContext(ctx, "run", "err", err)
	}
}

// New applies each registrar to the mux of the gateway, over the connection to
// the gRPC server. A service passes the generated registrars; this one is
// written by hand and reports the health of the server.
func ExampleNew() {
	srv := grpcserver.New(slog.New(slog.DiscardHandler), grpcserver.Config{})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println(err)
		return
	}
	go func() { _ = srv.Serve(lis) }()
	defer func() { _ = srv.Stop(context.Background()) }()

	health := func(_ context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error {
		client := healthpb.NewHealthClient(conn)
		return mux.HandlePath(http.MethodGet, "/v1/health",
			func(w http.ResponseWriter, r *http.Request, _ map[string]string) {
				resp, err := client.Check(r.Context(), &healthpb.HealthCheckRequest{})
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadGateway)
					return
				}
				fmt.Fprintf(w, `{"status":%q}`, resp.GetStatus())
			})
	}

	gw, err := grpcgateway.New(context.Background(),
		grpcgateway.Config{Endpoint: lis.Addr().String()}, health)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = gw.Close() }()

	w := httptest.NewRecorder()
	gw.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	fmt.Println(w.Code, w.Body.String())
	// Output:
	// 200 {"status":"SERVING"}
}
