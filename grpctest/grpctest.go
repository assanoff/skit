// Package grpctest spins up an in-memory gRPC server for tests, the gRPC
// counterpart of apitest. It runs a real grpcserver.Server — with the full skit
// interceptor chain (recovery, tracing, logging, and the errs.Error -> gRPC
// status mapping) — over an in-process bufconn listener, and hands back a
// *grpc.ClientConn dialed to it. No TCP port is bound, so tests are hermetic and
// free of port-conflict flakiness.
//
// Because the real interceptors run, an integration test asserts true gRPC
// status codes (status.Code(err)) end to end, which a direct handler call
// cannot: calling a handler method returns the raw *errs.Error, before the
// errormap interceptor converts it to a status.
//
// Usage:
//
//	srv := grpctest.New(t, grpcserver.Config{}, []grpcserver.Service{handler})
//	client := widgetv1.NewWidgetServiceClient(srv.Conn)
//	resp, err := client.GetWidget(ctx, &widgetv1.GetWidgetRequest{Id: id})
package grpctest

import (
	"context"
	"log/slog"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/assanoff/skit/grpcserver"
)

// bufSize is the bufconn buffer; 1 MiB comfortably fits test messages.
const bufSize = 1 << 20

// Server is an in-memory gRPC server plus a client connection to it. Conn is
// ready to build generated client stubs against. Cleanup is registered with the
// test, so callers need not close anything.
type Server struct {
	Conn *grpc.ClientConn

	srv *grpcserver.Server
	lis *bufconn.Listener
}

// New starts an in-memory gRPC server with svcs registered (each is a
// grpcserver.Service — the Register seam the scaffolded handler implements) and
// returns a Server whose Conn dials it over bufconn. cfg and opts are forwarded
// to grpcserver.New: cfg.TracerProvider records the spans of a test,
// grpcserver.WithUnaryInterceptors(authMW) adds an interceptor under test. The
// server and connection are torn down via t.Cleanup.
func New(t *testing.T, cfg grpcserver.Config, svcs []grpcserver.Service, opts ...grpcserver.Option) *Server {
	t.Helper()

	srv := grpcserver.New(slog.New(slog.DiscardHandler), cfg, opts...)
	srv.Install(svcs...)

	lis := bufconn.Listen(bufSize)
	go func() { _ = srv.Serve(lis) }()

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpctest: dial bufconn: %v", err)
	}

	t.Cleanup(func() {
		_ = conn.Close()
		_ = srv.Stop(context.Background())
		_ = lis.Close()
	})

	return &Server{Conn: conn, srv: srv, lis: lis}
}
