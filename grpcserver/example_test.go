package grpcserver_test

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/grpcserver"
	"github.com/assanoff/skit/metrics"
	"github.com/assanoff/skit/worker"
)

// A service runs the gRPC server in a worker group, next to the REST and
// status servers: Install registers the services of the features, and the
// group starts the server and drains it on SIGINT or SIGTERM.
func Example() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	// The handler of a feature; its Register calls the generated
	// widgetv1.RegisterWidgetServiceServer.
	var widgets grpcserver.Service

	srv := grpcserver.New(log, grpcserver.Config{
		Addr:             ":9090",
		EnableReflection: true,
		Metrics:          metrics.New(),
	})
	srv.Install(widgets)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	grp := worker.NewGroup(log, worker.GroupConfig{ShutdownTimeout: 10 * time.Second})
	grp.Add(srv)
	if err := grp.Run(ctx); err != nil {
		log.ErrorContext(ctx, "run", "err", err)
	}
}

// Serve takes a listener of the caller, here an ephemeral port. Every server
// serves the health service. An interceptor of WithUnaryInterceptors runs
// after the built-in ones, and the *errs.Error it returns reaches the client
// as a gRPC status.
func ExampleServer_Serve() {
	tenant := func(ctx context.Context, req any, info *grpc.UnaryServerInfo,
		h grpc.UnaryHandler,
	) (any, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		if len(md.Get("x-tenant")) == 0 {
			return nil, errs.Newf(errs.InvalidArgument, "%s: no x-tenant", info.FullMethod)
		}
		return h(ctx, req)
	}
	srv := grpcserver.New(slog.New(slog.DiscardHandler), grpcserver.Config{},
		grpcserver.WithUnaryInterceptors(tenant))

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println(err)
		return
	}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(lis) }()

	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = conn.Close() }()
	client := healthpb.NewHealthClient(conn)

	ctx := context.Background()
	_, err = client.Check(ctx, &healthpb.HealthCheckRequest{})
	fmt.Println(status.Code(err), status.Convert(err).Message())

	ctx = metadata.AppendToOutgoingContext(ctx, "x-tenant", "acme")
	resp, err := client.Check(ctx, &healthpb.HealthCheckRequest{})
	fmt.Println(resp.GetStatus(), err)

	fmt.Println(srv.Stop(context.Background()), <-served)
	// Output:
	// InvalidArgument /grpc.health.v1.Health/Check: no x-tenant
	// SERVING <nil>
	// <nil> <nil>
}
