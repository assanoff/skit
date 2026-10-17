package grpcgateway_test

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/matryer/is"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/assanoff/skit/grpcgateway"
	"github.com/assanoff/skit/grpcserver"
	"github.com/assanoff/skit/middleware"
)

// incoming is a traceparent from a caller's trace.
const incoming = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"

// echo is a hand-written service with one unary method, /test.Echo/Ping.
type echo struct{}

func (echo) Register(reg grpc.ServiceRegistrar) {
	reg.RegisterService(&grpc.ServiceDesc{
		ServiceName: "test.Echo",
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "Ping",
			Handler:    ping,
		}},
	}, echo{})
}

// ping is the method handler as protoc-gen-go-grpc writes it.
//
//nolint:revive // the signature of grpc.MethodDesc.Handler puts ctx second
func ping(srv any, ctx context.Context, dec func(any) error, in grpc.UnaryServerInterceptor) (any, error) {
	req := &emptypb.Empty{}
	if err := dec(req); err != nil {
		return nil, err
	}
	call := func(context.Context, any) (any, error) {
		return &emptypb.Empty{}, nil
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/test.Echo/Ping"}
	return in(ctx, req, info, call)
}

// pingRoute maps GET /v1/echo/{name} to /test.Echo/Ping, as a generated
// registrar maps a google.api.http rule.
func pingRoute(_ context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error {
	return mux.HandlePath(http.MethodGet, "/v1/echo/{name}",
		func(w http.ResponseWriter, r *http.Request, _ map[string]string) {
			err := conn.Invoke(r.Context(), "/test.Echo/Ping", &emptypb.Empty{}, &emptypb.Empty{})
			if err != nil {
				w.WriteHeader(http.StatusBadGateway)
			}
		})
}

// A request through the gateway is one trace: the HTTP server span, named
// after the gateway route, the client span of the call, and the gRPC server
// span.
func TestGatewayOneTrace(t *testing.T) {
	is := is.New(t)
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))

	srv := grpcserver.New(slog.New(slog.DiscardHandler), grpcserver.Config{TracerProvider: tp})
	srv.Install(echo{})
	lis := bufconn.Listen(1 << 20)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() { _ = srv.Stop(context.Background()) })

	gw, err := grpcgateway.New(t.Context(), grpcgateway.Config{
		Endpoint: "passthrough:///bufnet",
		DialOptions: []grpc.DialOption{
			grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
				return lis.DialContext(ctx)
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		},
		TracerProvider: tp,
	}, pingRoute)
	is.NoErr(err)
	t.Cleanup(func() { _ = gw.Close() })
	// Mounted on a prefix, as the template mounts it.
	mux := http.NewServeMux()
	mux.Handle("/v1/", middleware.Trace(middleware.TraceConfig{TracerProvider: tp})(gw))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/echo/abc", nil)
	req.Header.Set("Traceparent", incoming)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	is.Equal(w.Code, http.StatusOK)

	spans := rec.Ended()
	is.Equal(len(spans), 3)
	for _, s := range spans {
		is.Equal(s.SpanContext().TraceID().String(), "0af7651916cd43dd8448eb211c80319c")
	}
	httpSpan := find(t, spans, trace.SpanKindServer, "GET /v1/echo/{name}")
	client := find(t, spans, trace.SpanKindClient, "test.Echo/Ping")
	rpc := find(t, spans, trace.SpanKindServer, "test.Echo/Ping")
	is.Equal(route(httpSpan), "/v1/echo/{name}")
	is.Equal(client.Parent().SpanID(), httpSpan.SpanContext().SpanID())
	is.Equal(rpc.Parent().SpanID(), client.SpanContext().SpanID())
}

// find returns the span of kind named name.
func find(t *testing.T, spans []sdktrace.ReadOnlySpan, kind trace.SpanKind, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, s := range spans {
		if s.SpanKind() == kind && s.Name() == name {
			return s
		}
	}
	t.Fatalf("no %s span %q", kind, name)
	return nil
}

// route returns the http.route of span, or "".
func route(span sdktrace.ReadOnlySpan) string {
	for _, kv := range span.Attributes() {
		if kv.Key == semconv.HTTPRouteKey {
			return kv.Value.AsString()
		}
	}
	return ""
}
