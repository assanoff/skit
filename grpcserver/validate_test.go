package grpcserver

import (
	"context"
	"errors"
	"testing"

	"buf.build/go/protovalidate"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/assanoff/skit/errs"
)

// passHandler records that it ran and returns a sentinel response.
func passHandler(called *bool) grpc.UnaryHandler {
	return func(_ context.Context, _ any) (any, error) {
		*called = true
		return "ok", nil
	}
}

func TestValidateUnaryPassesValidMessage(t *testing.T) {
	v, err := protovalidate.New()
	if err != nil {
		t.Fatalf("protovalidate.New: %v", err)
	}
	in := validateUnary(v, nil)

	called := false
	// emptypb.Empty carries no buf.validate rules, so validation succeeds and the
	// handler runs.
	resp, err := in(context.Background(), &emptypb.Empty{}, &grpc.UnaryServerInfo{}, passHandler(&called))
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
	if !called {
		t.Fatal("handler was not called for a valid message")
	}
	if resp != "ok" {
		t.Fatalf("resp = %v, want ok", resp)
	}
}

func TestValidateUnaryPassesNonProtoRequest(t *testing.T) {
	v, err := protovalidate.New()
	if err != nil {
		t.Fatalf("protovalidate.New: %v", err)
	}
	in := validateUnary(v, nil)

	called := false
	// A non-proto request cannot be validated; the interceptor must not block it.
	if _, err := in(context.Background(), "not a proto", &grpc.UnaryServerInfo{}, passHandler(&called)); err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
	if !called {
		t.Fatal("handler was not called for a non-proto request")
	}
}

func TestValidateUnaryReportsBuildError(t *testing.T) {
	in := validateUnary(nil, errors.New("boom"))

	called := false
	_, err := in(context.Background(), &emptypb.Empty{}, &grpc.UnaryServerInfo{}, passHandler(&called))
	if err == nil {
		t.Fatal("expected an error when the validator failed to build")
	}
	if called {
		t.Fatal("handler must not run when the validator is unavailable")
	}
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.Internal {
		t.Fatalf("want errs.Internal, got %v", err)
	}
}
