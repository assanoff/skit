package grpcserver

import (
	"testing"

	"github.com/matryer/is"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

// The technical services — health and reflection — get no spans; the
// application's methods do.
func TestMethodSetSkipsTechnicalServices(t *testing.T) {
	is := is.New(t)
	gs := grpc.NewServer()
	healthpb.RegisterHealthServer(gs, health.NewServer())
	reflection.Register(gs)
	gs.RegisterService(&grpc.ServiceDesc{
		ServiceName: "test.Echo",
		HandlerType: (*any)(nil),
		Methods:     []grpc.MethodDesc{{MethodName: "Ping"}},
	}, struct{}{})

	var m methodSet
	is.True(!m.has("/test.Echo/Ping")) // nothing before Serve loads the set
	m.load(gs)

	is.True(m.has("/test.Echo/Ping"))
	is.True(!m.has("/grpc.health.v1.Health/Check"))
	is.True(!m.has("/grpc.reflection.v1.ServerReflection/ServerReflectionInfo"))
	is.True(!m.has("/test.Echo/Missing"))
}
