// Package grpctest runs an in-memory gRPC server for tests, the gRPC
// counterpart of apitest. It serves a real grpcserver.Server — the full skit
// interceptor chain: recovery, tracing, logging and the mapping of an
// errs.Error to a gRPC status — over an in-process bufconn listener, and hands
// back a *grpc.ClientConn dialed to it. No TCP port is bound, so the tests
// are hermetic and free of port conflicts.
//
// Because the real interceptors run, a test asserts the gRPC status codes a
// client gets (status.Code(err)), which a direct call of a handler cannot: the
// handler returns the *errs.Error before the interceptor maps it to a status.
//
// # Usage
//
//	func TestGetWidget(t *testing.T) {
//	    srv := grpctest.New(t, grpcserver.Config{}, []grpcserver.Service{handler})
//	    client := widgetv1.NewWidgetServiceClient(srv.Conn)
//
//	    _, err := client.GetWidget(t.Context(), &widgetv1.GetWidgetRequest{Id: "x"})
//	    if status.Code(err) != codes.NotFound {
//	        t.Fatalf("got %v, want NotFound", err)
//	    }
//	}
//
// New forwards cfg and the options to grpcserver.New: cfg.TracerProvider
// records the spans of a test, grpcserver.WithUnaryInterceptors adds an
// interceptor under test. The server and the connection close with the test
// (t.Cleanup).
package grpctest
