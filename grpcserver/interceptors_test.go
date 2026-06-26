package grpcserver

import (
	"context"
	"errors"
	"testing"

	"github.com/matryer/is"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/assanoff/skit/errs"
)

func TestToStatus(t *testing.T) {
	is := is.New(t)

	// nil stays nil (status.FromError reports a nil error as ok).
	is.True(toStatus(nil) == nil)

	// errs.Error -> a status with the aligned code and the (sanitized) message.
	mapped := toStatus(errs.Newf(errs.NotFound, "widget %d", 7))
	st, ok := status.FromError(mapped)
	is.True(ok)
	is.Equal(st.Code(), codes.NotFound) // errs.NotFound aligns with codes.NotFound
	is.Equal(st.Message(), "widget 7")

	// A non-errs, non-status error is hidden behind Internal.
	generic := toStatus(errors.New("raw db failure"))
	gst, _ := status.FromError(generic)
	is.Equal(gst.Code(), codes.Internal)
	is.Equal(gst.Message(), "internal error")

	// An error that is already a status passes through unchanged.
	orig := status.Error(codes.PermissionDenied, "nope")
	is.True(errors.Is(toStatus(orig), orig))
}

func TestRecoveryUnaryConvertsPanic(t *testing.T) {
	is := is.New(t)

	interceptor := recoveryUnary(nil) // a nil logger must be safe
	handler := func(ctx context.Context, req any) (any, error) {
		panic("boom")
	}

	resp, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/svc/M"}, handler)

	is.True(resp == nil)
	st, ok := status.FromError(err)
	is.True(ok)
	is.Equal(st.Code(), codes.Internal) // a panic becomes Internal, not a crash
}

func TestErrorMapUnary(t *testing.T) {
	is := is.New(t)

	interceptor := errorMapUnary()

	// A returned errs.Error is mapped to its gRPC status.
	failing := func(ctx context.Context, req any) (any, error) {
		return nil, errs.Newf(errs.InvalidArgument, "bad input")
	}
	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{}, failing)
	st, _ := status.FromError(err)
	is.Equal(st.Code(), codes.InvalidArgument)

	// A nil error (and its response) passes through untouched.
	okHandler := func(ctx context.Context, req any) (any, error) {
		return "v", nil
	}
	resp, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{}, okHandler)
	is.NoErr(err)
	is.Equal(resp, "v")
}
