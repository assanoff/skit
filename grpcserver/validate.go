package grpcserver

import (
	"context"

	"buf.build/go/protovalidate"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.com/assanoff/skit/errs"
)

// WithProtoValidate installs a unary interceptor that validates every request
// message against its buf.validate rules (protovalidate) before the handler
// runs. A violation is returned as errs.InvalidArgument, which the built-in
// error-map interceptor turns into a gRPC InvalidArgument status — so handlers
// need not re-check the constraints declared in the .proto.
//
// The validator is built once and is safe for concurrent use. If the CEL
// environment fails to build (a misconfiguration), the interceptor fails every
// request with Internal rather than silently skipping validation.
func WithProtoValidate() Option {
	v, err := protovalidate.New()
	return func(o *options) {
		o.unaryExtra = append(o.unaryExtra, validateUnary(v, err))
	}
}

func validateUnary(v protovalidate.Validator, buildErr error) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if buildErr != nil {
			return nil, errs.Newf(errs.Internal, "protovalidate: %s", buildErr)
		}
		if msg, ok := req.(proto.Message); ok {
			if err := v.Validate(msg); err != nil {
				return nil, errs.Newf(errs.InvalidArgument, "%s", err)
			}
		}
		return handler(ctx, req)
	}
}
