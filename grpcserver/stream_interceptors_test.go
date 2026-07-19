package grpcserver

import (
	"context"
	"testing"

	"github.com/matryer/is"
	"go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/assanoff/skit/errs"
)

// fakeStream is a minimal grpc.ServerStream for interceptor tests. Only Context
// is exercised; the embedded nil interface makes unused methods a clear panic.
type fakeStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (f *fakeStream) Context() context.Context {
	if f.ctx != nil {
		return f.ctx
	}
	return context.Background()
}

func TestRecoveryStreamConvertsPanic(t *testing.T) {
	is := is.New(t)

	interceptor := recoveryStream(nil) // nil logger must be safe
	handler := func(srv any, ss grpc.ServerStream) error { panic("boom") }

	err := interceptor(nil, &fakeStream{}, &grpc.StreamServerInfo{FullMethod: "/svc/S"}, handler)
	st, ok := status.FromError(err)
	is.True(ok)
	is.Equal(st.Code(), codes.Internal) // a panic becomes Internal, not a crash
}

func TestErrorMapStream(t *testing.T) {
	is := is.New(t)

	interceptor := errorMapStream()

	failing := func(srv any, ss grpc.ServerStream) error { return errs.Newf(errs.InvalidArgument, "bad input") }
	err := interceptor(nil, &fakeStream{}, &grpc.StreamServerInfo{}, failing)
	st, _ := status.FromError(err)
	is.Equal(st.Code(), codes.InvalidArgument)

	okHandler := func(srv any, ss grpc.ServerStream) error { return nil }
	is.NoErr(interceptor(nil, &fakeStream{}, &grpc.StreamServerInfo{}, okHandler))
}

// traceStream must hand the handler a stream whose Context carries over the
// inbound values (via wrappedStream) plus the injected tracing.
func TestTraceStreamWrapsContext(t *testing.T) {
	is := is.New(t)

	type ctxKey struct{}
	base := context.WithValue(context.Background(), ctxKey{}, "v")
	interceptor := traceStream(noop.NewTracerProvider().Tracer("test"))

	var seen context.Context
	handler := func(srv any, ss grpc.ServerStream) error { seen = ss.Context(); return nil }
	err := interceptor(nil, &fakeStream{ctx: base}, &grpc.StreamServerInfo{}, handler)

	is.NoErr(err)
	is.Equal(seen.Value(ctxKey{}), "v") // wrapped context preserves inbound values
}

// WithStreamInterceptors threads application stream interceptors into the built
// chain (closest to the handler).
func TestWithStreamInterceptorsOption(t *testing.T) {
	is := is.New(t)

	var o options
	called := false
	mw := func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, h grpc.StreamHandler) error {
		called = true
		return h(srv, ss)
	}
	WithStreamInterceptors(mw)(&o)

	is.Equal(len(o.streamExtra), 1)
	_ = o.streamExtra[0](nil, &fakeStream{}, &grpc.StreamServerInfo{}, func(any, grpc.ServerStream) error { return nil })
	is.True(called)
}
