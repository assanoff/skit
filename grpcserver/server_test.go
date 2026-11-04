package grpcserver_test

import (
	"context"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"github.com/matryer/is"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/assanoff/skit/grpcserver"
)

func TestServerIdentity(t *testing.T) {
	is := is.New(t)
	srv := grpcserver.New(slog.New(slog.DiscardHandler), grpcserver.Config{Addr: ":9090"})

	is.Equal(srv.Name(), "grpc-server")
	is.Equal(srv.Addr(), ":9090") // the configured address, not the bound one
}

// A service registered on ServiceRegistrar is served like one from Install.
func TestServiceRegistrar(t *testing.T) {
	is := is.New(t)
	srv := grpcserver.New(slog.New(slog.DiscardHandler), grpcserver.Config{})
	outcomes{}.Register(srv.ServiceRegistrar())
	conn := dial(t, srv)

	err := conn.Invoke(t.Context(), "/test.Outcomes/Ok", &emptypb.Empty{}, &emptypb.Empty{})
	is.NoErr(err)
}

func TestStartServesUntilStop(t *testing.T) {
	is := is.New(t)
	addr := freeAddr(t)
	srv := grpcserver.New(slog.New(slog.DiscardHandler), grpcserver.Config{Addr: addr})
	done := make(chan error, 1)
	go func() { done <- srv.Start(context.Background()) }()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	is.NoErr(err)
	defer func() { _ = conn.Close() }()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	resp, err := healthpb.NewHealthClient(conn).
		Check(ctx, &healthpb.HealthCheckRequest{}, grpc.WaitForReady(true))
	is.NoErr(err)
	is.Equal(resp.GetStatus(), healthpb.HealthCheckResponse_SERVING)

	is.NoErr(srv.Stop(context.Background()))
	is.NoErr(<-done) // Start returns nil once stopped
}

func TestStartFailsOnBusyAddr(t *testing.T) {
	is := is.New(t)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	is.NoErr(err)
	defer func() { _ = lis.Close() }()
	srv := grpcserver.New(slog.New(slog.DiscardHandler),
		grpcserver.Config{Addr: lis.Addr().String()})

	err = srv.Start(context.Background())
	is.True(err != nil)
	is.True(strings.Contains(err.Error(), "listen")) // the bind failed
}

// freeAddr returns a loopback address with a port free a moment ago.
func freeAddr(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := lis.Addr().String()
	_ = lis.Close()
	return addr
}

// A request that breaks a buf.validate rule of its message never reaches the
// handler and ends with InvalidArgument; a valid one passes.
func TestWithProtoValidate(t *testing.T) {
	is := is.New(t)
	widget := widgetDescriptor(t)
	var handled int
	srv := grpcserver.New(slog.New(slog.DiscardHandler), grpcserver.Config{},
		grpcserver.WithProtoValidate())
	srv.Install(widgets{msg: widget, handled: &handled})
	conn := dial(t, srv)

	create := func(name string) error {
		req := dynamicpb.NewMessage(widget)
		req.Set(widget.Fields().ByName("name"), protoreflect.ValueOfString(name))
		return conn.Invoke(t.Context(), "/test.Widgets/Create", req, dynamicpb.NewMessage(widget))
	}
	is.Equal(status.Code(create("")), codes.InvalidArgument) // min_len 1
	is.Equal(handled, 0)                                     // the handler did not run
	is.NoErr(create("gear"))
	is.Equal(handled, 1)
}

// widgetDescriptor describes the message test.Widget with one field,
// string name = 1 [(buf.validate.field).string.min_len = 1], built here
// instead of generated from a .proto.
func widgetDescriptor(t *testing.T) protoreflect.MessageDescriptor {
	t.Helper()
	opts := &descriptorpb.FieldOptions{}
	proto.SetExtension(opts, validate.E_Field, &validate.FieldRules{
		Type: &validate.FieldRules_String_{
			String_: &validate.StringRules{MinLen: new(uint64(1))},
		},
	})
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:    new("test/widget.proto"),
		Package: new("test"),
		Syntax:  new("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: new("Widget"),
			Field: []*descriptorpb.FieldDescriptorProto{{
				Name:     new("name"),
				JsonName: new("name"),
				Number:   new(int32(1)),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				Options:  opts,
			}},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("build descriptor: %v", err)
	}
	return file.Messages().Get(0)
}

// widgets is the service test.Widgets with the unary method Create, which
// takes and returns a test.Widget and counts its calls in handled.
type widgets struct {
	msg     protoreflect.MessageDescriptor
	handled *int
}

func (w widgets) Register(reg grpc.ServiceRegistrar) {
	create := func(srv any, ctx context.Context, dec func(any) error, in grpc.UnaryServerInterceptor) (any, error) {
		req := dynamicpb.NewMessage(w.msg)
		if err := dec(req); err != nil {
			return nil, err
		}
		call := func(_ context.Context, req any) (any, error) {
			*w.handled++
			return req, nil
		}
		info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/test.Widgets/Create"}
		return in(ctx, req, info, call)
	}
	reg.RegisterService(&grpc.ServiceDesc{
		ServiceName: "test.Widgets",
		HandlerType: (*any)(nil),
		Methods:     []grpc.MethodDesc{{MethodName: "Create", Handler: create}},
	}, w)
}
