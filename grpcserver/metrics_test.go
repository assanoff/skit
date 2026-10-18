package grpcserver_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/matryer/is"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/grpcserver"
)

// outcomes is a hand-written service whose methods end as they are named:
// /test.Outcomes/Ok, /test.Outcomes/NotFound and /test.Outcomes/Panic.
type outcomes struct{}

func (outcomes) Register(reg grpc.ServiceRegistrar) {
	reg.RegisterService(&grpc.ServiceDesc{
		ServiceName: "test.Outcomes",
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{
			outcome("Ok", func() error { return nil }),
			outcome("NotFound", func() error {
				return errs.Newf(errs.NotFound, "missing")
			}),
			outcome("Panic", func() error { panic("boom") }),
		},
	}, outcomes{})
}

// outcome is a unary method of test.Outcomes whose handler returns end().
func outcome(name string, end func() error) grpc.MethodDesc {
	return grpc.MethodDesc{
		MethodName: name,
		Handler: func(srv any, ctx context.Context, dec func(any) error, in grpc.UnaryServerInterceptor) (any, error) {
			req := &emptypb.Empty{}
			if err := dec(req); err != nil {
				return nil, err
			}
			call := func(context.Context, any) (any, error) {
				return &emptypb.Empty{}, end()
			}
			info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/test.Outcomes/" + name}
			return in(ctx, req, info, call)
		},
	}
}

// Every served RPC is counted by method and canonical code, a panic as
// INTERNAL; health checks are not.
func TestMetrics(t *testing.T) {
	is := is.New(t)
	reg := prometheus.NewRegistry()
	srv := grpcserver.New(slog.New(slog.DiscardHandler), grpcserver.Config{Metrics: reg})
	srv.Install(outcomes{})
	conn := dial(t, srv)

	calls := []struct {
		method string
		want   codes.Code
	}{
		{method: "/test.Outcomes/Ok", want: codes.OK},
		{method: "/test.Outcomes/NotFound", want: codes.NotFound},
		{method: "/test.Outcomes/Panic", want: codes.Internal},
	}
	for _, c := range calls {
		err := conn.Invoke(t.Context(), c.method, &emptypb.Empty{}, &emptypb.Empty{})
		is.Equal(status.Code(err), c.want) // the status the method ends with
	}
	_, err := healthpb.NewHealthClient(conn).Check(t.Context(), &healthpb.HealthCheckRequest{})
	is.NoErr(err)

	want := `
# HELP skit_grpc_server_handled_total RPCs completed, by method and status code.
# TYPE skit_grpc_server_handled_total counter
skit_grpc_server_handled_total{code="INTERNAL",method="test.Outcomes/Panic"} 1
skit_grpc_server_handled_total{code="NOT_FOUND",method="test.Outcomes/NotFound"} 1
skit_grpc_server_handled_total{code="OK",method="test.Outcomes/Ok"} 1
# HELP skit_grpc_server_in_flight RPCs being handled.
# TYPE skit_grpc_server_in_flight gauge
skit_grpc_server_in_flight 0
`
	is.NoErr(testutil.GatherAndCompare(reg, strings.NewReader(want),
		"skit_grpc_server_handled_total", "skit_grpc_server_in_flight"))
	n, err := testutil.GatherAndCount(reg, "skit_grpc_server_handling_seconds")
	is.NoErr(err)
	is.Equal(n, 3) // a duration series per method
}

// Two servers on one registry share the metrics instead of failing to register.
func TestMetricsSharedRegistry(t *testing.T) {
	is := is.New(t)
	reg := prometheus.NewRegistry()
	log := slog.New(slog.DiscardHandler)
	grpcserver.New(log, grpcserver.Config{Metrics: reg})
	srv := grpcserver.New(log, grpcserver.Config{Metrics: reg})
	srv.Install(outcomes{})
	conn := dial(t, srv)

	is.NoErr(conn.Invoke(t.Context(), "/test.Outcomes/Ok", &emptypb.Empty{}, &emptypb.Empty{}))

	n, err := testutil.GatherAndCount(reg, "skit_grpc_server_handled_total")
	is.NoErr(err)
	is.Equal(n, 1)
}
