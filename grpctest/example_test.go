package grpctest_test

import (
	"context"
	"testing"

	"github.com/matryer/is"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/grpcserver"
	"github.com/assanoff/skit/grpctest"
)

// A test calls the server through Conn and checks the status code the client
// gets: the interceptors run as in a service, the mapping of an *errs.Error
// included. Here an interceptor under test guards the health service; a test
// of a feature passes its handler in svcs.
func Example() {
	var t *testing.T // the *testing.T of the test
	is := is.New(t)
	ctx := context.Background() // t.Context() in a test

	tenant := func(ctx context.Context, req any, _ *grpc.UnaryServerInfo,
		h grpc.UnaryHandler,
	) (any, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		if len(md.Get("x-tenant")) == 0 {
			return nil, errs.Newf(errs.InvalidArgument, "no x-tenant")
		}
		return h(ctx, req)
	}
	srv := grpctest.New(t, grpcserver.Config{}, nil, grpcserver.WithUnaryInterceptors(tenant))
	client := healthpb.NewHealthClient(srv.Conn)

	_, err := client.Check(ctx, &healthpb.HealthCheckRequest{})
	is.Equal(status.Code(err), codes.InvalidArgument)

	resp, err := client.Check(metadata.AppendToOutgoingContext(ctx, "x-tenant", "acme"),
		&healthpb.HealthCheckRequest{})
	is.NoErr(err)
	is.Equal(resp.GetStatus(), healthpb.HealthCheckResponse_SERVING)
}
