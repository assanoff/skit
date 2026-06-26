package grpcserver

import (
	"context"
	"errors"
	"runtime/debug"
	"time"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/logger"
	skotel "github.com/assanoff/skit/otel"
)

// recoveryUnary turns a panic in a handler into an Internal status instead of
// crashing the server.
func recoveryUnary(log *logger.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				if log != nil {
					log.Error(ctx, "grpc handler panic recovered",
						"method", info.FullMethod, "panic", r, "stack", string(debug.Stack()))
				}
				err = status.Errorf(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}

// traceUnary extracts inbound trace context from gRPC metadata and ensures a
// trace id is present in the context so logs can carry it.
func traceUnary(tracer trace.Tracer) grpc.UnaryServerInterceptor {
	prop := propagation.TraceContext{}
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			ctx = prop.Extract(ctx, metadataCarrier(md))
		}
		ctx = skotel.InjectTracing(ctx, tracer)
		return handler(ctx, req)
	}
}

// loggingUnary logs one structured line per RPC, including the (already mapped)
// status code, latency, and the request's trace_id.
func loggingUnary(log *logger.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		if log != nil {
			log.Info(ctx, "grpc.request",
				"rpc.method", info.FullMethod,
				"rpc.grpc.status_code", status.Code(err).String(),
				"duration_ms", time.Since(start).Milliseconds(),
			)
		}
		return resp, err
	}
}

// metricsUnary records count and latency per method and status code.
func metricsUnary(m *rpcMetrics) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		m.observe(info.FullMethod, status.Code(err).String(), float64(time.Since(start).Milliseconds()))
		return resp, err
	}
}

// errorMapUnary converts a returned error into a gRPC status. Because errs.Code
// values are aligned with gRPC codes, the mapping is a direct cast. Errors that
// are already a gRPC status pass through unchanged.
func errorMapUnary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		resp, err := handler(ctx, req)
		if err == nil {
			return resp, nil
		}
		return resp, toStatus(err)
	}
}

// toStatus maps any error to a gRPC status error.
func toStatus(err error) error {
	if _, ok := status.FromError(err); ok {
		return err // already a status (or nil)
	}
	var e *errs.Error
	if errors.As(err, &e) {
		return status.Error(codes.Code(e.Code), errs.Sanitize(e.Message))
	}
	return status.Error(codes.Internal, "internal error")
}

// metadataCarrier adapts gRPC metadata to a propagation.TextMapCarrier.
type metadataCarrier metadata.MD

func (c metadataCarrier) Get(key string) string {
	v := metadata.MD(c).Get(key)
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

func (c metadataCarrier) Set(key, value string) { metadata.MD(c).Set(key, value) }

func (c metadataCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}
