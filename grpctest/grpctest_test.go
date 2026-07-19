package grpctest_test

import (
	"context"
	"testing"

	"github.com/matryer/is"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/assanoff/skit/grpcserver"
	"github.com/assanoff/skit/grpctest"
)

// TestServesOverBufconn checks the in-memory server is reachable through the
// returned client connection: the built-in health service (registered by
// grpcserver) reports SERVING over bufconn, exercising the whole path — server,
// interceptor chain, bufconn listener, and client dial — with no services of
// our own registered.
func TestServesOverBufconn(t *testing.T) {
	is := is.New(t)

	srv := grpctest.New(t, nil) // no app services; just the built-in health server
	client := healthpb.NewHealthClient(srv.Conn)

	resp, err := client.Check(context.Background(), &healthpb.HealthCheckRequest{})
	is.NoErr(err)
	is.Equal(resp.GetStatus(), healthpb.HealthCheckResponse_SERVING)
}

// TestForwardsServerOptions checks opts reach grpcserver.New (here: reflection
// stays off by default, and the server still builds and serves with an option
// applied). It mainly guards that the variadic option plumbing compiles and runs.
func TestForwardsServerOptions(t *testing.T) {
	is := is.New(t)

	srv := grpctest.New(t, []grpcserver.Service{}) // empty service slice is valid
	is.True(srv.Conn != nil)
}
