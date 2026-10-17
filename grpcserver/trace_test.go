package grpcserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"regexp"
	"strings"
	"testing"

	"github.com/matryer/is"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/assanoff/skit/grpcserver"
	"github.com/assanoff/skit/logger"
	"github.com/assanoff/skit/otel/setup"
)

// hex32 matches a W3C trace id.
var hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)

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

// ping is the method handler as protoc-gen-go-grpc writes it: the server's
// interceptor chain runs around the call.
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

// dial serves srv over bufconn and returns a client connection to it.
func dial(t *testing.T, srv *grpcserver.Server) *grpc.ClientConn {
	t.Helper()
	srv.Install(echo{})
	lis := bufconn.Listen(1 << 20)
	go func() { _ = srv.Serve(lis) }()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		_ = srv.Stop(context.Background())
	})
	return conn
}

// One server span per served RPC; health checks and methods the server does
// not serve get none.
func TestServerSpans(t *testing.T) {
	is := is.New(t)
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	conn := dial(t, grpcserver.New(slog.New(slog.DiscardHandler),
		grpcserver.Config{TracerProvider: tp}))
	const incoming = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	ctx := metadata.AppendToOutgoingContext(t.Context(), "traceparent", incoming)

	is.NoErr(conn.Invoke(ctx, "/test.Echo/Ping", &emptypb.Empty{}, &emptypb.Empty{}))
	_, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	is.NoErr(err)
	err = conn.Invoke(ctx, "/test.Echo/Missing", &emptypb.Empty{}, &emptypb.Empty{})
	is.Equal(status.Code(err), codes.Unimplemented)

	spans := rec.Ended()
	is.Equal(len(spans), 1)
	is.Equal(spans[0].Name(), "test.Echo/Ping")
	is.Equal(spans[0].SpanKind(), trace.SpanKindServer)
	is.Equal(spans[0].SpanContext().TraceID().String(), "0af7651916cd43dd8448eb211c80319c")
}

// With tracing not exported, the RPC log line still carries a W3C trace id
// with trace flags 00.
func TestLogCarriesTraceIDWithoutExport(t *testing.T) {
	is := is.New(t)
	tel, err := setup.New(t.Context(), slog.New(slog.DiscardHandler), setup.Config{})
	is.NoErr(err)
	defer func() { _ = tel.Shutdown() }()
	var buf bytes.Buffer
	log := logger.New(&buf, logger.Config{Level: logger.LevelInfo})
	conn := dial(t, grpcserver.New(log,
		grpcserver.Config{TracerProvider: tel.TracerProvider()}))

	is.NoErr(conn.Invoke(t.Context(), "/test.Echo/Ping", &emptypb.Empty{}, &emptypb.Empty{}))

	var line map[string]any
	for ln := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		is.NoErr(json.Unmarshal([]byte(ln), &m))
		if m["msg"] == "grpc.request" {
			line = m
		}
	}
	traceID, _ := line["trace_id"].(string)
	is.True(hex32.MatchString(traceID))
	is.Equal(line["trace_flags"], "00")
}
