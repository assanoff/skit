package grpcserver

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/logger"
)

// recoveryUnary turns a panic in a handler into an Internal status instead of
// crashing the server.
func recoveryUnary(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				if log != nil {
					log.ErrorContext(ctx, "grpc handler panic recovered",
						logger.PanicArgs(r, "method", info.FullMethod)...)
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
			log.InfoContext(ctx, "grpc.request", rpcLogArgs(info.FullMethod, err, time.Since(start))...)
		}
		return resp, err
	}
}

// rpcLogArgs are the fields of the log line of an RPC of fullMethod that ended
// with err after d, under the names of the RPC semantic conventions: the
// method without its leading "/", the canonical status code ("NOT_FOUND") and
// the duration in seconds — the values the metrics record.
func rpcLogArgs(fullMethod string, err error, d time.Duration) []any {
	return []any{
		string(semconv.RPCMethodKey), strings.TrimPrefix(fullMethod, "/"),
		string(semconv.RPCResponseStatusCodeKey), codeName(status.Code(err)),
		"rpc.server.call.duration", d.Seconds(),
	}
}

// metricsUnary records the RPCs of the served methods. It is the outermost
// interceptor, so an RPC whose handler panicked is recorded with the Internal
// status recovery returns.
func metricsUnary(m *rpcMetrics) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !m.methods.has(info.FullMethod) {
			return handler(ctx, req)
		}
		start := time.Now()
		m.inFlight.Inc()
		resp, err := handler(ctx, req)
		m.inFlight.Dec()
		m.record(ctx, info.FullMethod, err, time.Since(start))
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
						logger.PanicArgs(r, "method", info.FullMethod)...)
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
			log.InfoContext(ss.Context(), "grpc.stream", rpcLogArgs(info.FullMethod, err, time.Since(start))...)
		}
		return err
	}
}

// metricsStream records the stream RPCs of the served methods, mirroring
// metricsUnary.
func metricsStream(m *rpcMetrics) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if !m.methods.has(info.FullMethod) {
			return handler(srv, ss)
		}
		start := time.Now()
		m.inFlight.Inc()
		err := handler(srv, ss)
		m.inFlight.Dec()
		m.record(ss.Context(), info.FullMethod, err, time.Since(start))
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
