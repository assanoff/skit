package grpcserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/assanoff/skit/errs"
)

// recoveryUnary turns a panic in a handler into an Internal status instead of
// crashing the server.
func recoveryUnary(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				if log != nil {
					log.ErrorContext(ctx, "grpc handler panic recovered",
						"method", info.FullMethod, "err", fmt.Errorf("panic: %v", r),
						"exception.stacktrace", string(debug.Stack()))
				}
				err = status.Errorf(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}

// loggingUnary logs one structured line per RPC, including the (already mapped)
// status code, latency, and the request's trace_id.
func loggingUnary(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		if log != nil {
			log.InfoContext(ctx, "grpc.request",
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
	if e, ok := errors.AsType[*errs.Error](err); ok {
		return status.Error(codes.Code(e.Code), errs.Sanitize(e.Message))
	}
	return status.Error(codes.Internal, "internal error")
}

// --- stream interceptors (symmetric to the unary chain above) ---

// recoveryStream turns a panic in a streaming handler into an Internal status
// instead of crashing the server.
func recoveryStream(log *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if r := recover(); r != nil {
				if log != nil {
					log.ErrorContext(ss.Context(), "grpc stream handler panic recovered",
						"method", info.FullMethod, "err", fmt.Errorf("panic: %v", r),
						"exception.stacktrace", string(debug.Stack()))
				}
				err = status.Errorf(codes.Internal, "internal error")
			}
		}()
		return handler(srv, ss)
	}
}

// loggingStream logs one structured line per stream RPC (the already-mapped
// status code, duration and trace_id).
func loggingStream(log *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		err := handler(srv, ss)
		if log != nil {
			log.InfoContext(ss.Context(), "grpc.stream",
				"rpc.method", info.FullMethod,
				"rpc.grpc.status_code", status.Code(err).String(),
				"duration_ms", time.Since(start).Milliseconds(),
			)
		}
		return err
	}
}

// metricsStream records count and latency per method and status code.
func metricsStream(m *rpcMetrics) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		err := handler(srv, ss)
		m.observe(info.FullMethod, status.Code(err).String(), float64(time.Since(start).Milliseconds()))
		return err
	}
}

// errorMapStream converts a returned error into a gRPC status, mirroring
// errorMapUnary.
func errorMapStream() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := handler(srv, ss); err != nil {
			return toStatus(err)
		}
		return nil
	}
}
