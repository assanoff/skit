package grpcserver

import (
	"context"
	"net"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

	"github.com/assanoff/skit/logger"
)

// Config configures the gRPC server.
type Config struct {
	// Addr is the listen address, e.g. ":9090".
	Addr string
	// ShutdownTimeout bounds graceful shutdown before a hard stop (default 10s).
	ShutdownTimeout time.Duration
	// EnableReflection turns on server reflection (handy for grpcurl in dev).
	EnableReflection bool
	// MetricsNamespace prefixes the gRPC metric names (default "grpc").
	MetricsNamespace string

	// --- performance / tuning ---

	// MaxRecvMsgSize / MaxSendMsgSize override the 4 MiB defaults (bytes).
	MaxRecvMsgSize int
	MaxSendMsgSize int
	// NumStreamWorkers bounds the pool of goroutines serving streams; 0 spawns
	// one goroutine per stream (the gRPC default).
	NumStreamWorkers uint32
	// SharedWriteBuffer reuses transport write buffers across RPCs to cut
	// allocations under load.
	SharedWriteBuffer bool
	// Keepalive tunes connection liveness; zero values fall back to gRPC defaults.
	Keepalive KeepaliveConfig
}

// KeepaliveConfig mirrors the gRPC keepalive server parameters and enforcement
// policy. Zero values are left at gRPC defaults.
type KeepaliveConfig struct {
	MaxConnectionIdle   time.Duration
	MaxConnectionAge    time.Duration
	Time                time.Duration
	Timeout             time.Duration
	MinTime             time.Duration
	PermitWithoutStream bool
}

// Server wraps a *grpc.Server and implements worker.Runnable.
type Server struct {
	log    *logger.Logger
	cfg    Config
	gs     *grpc.Server
	health *health.Server
}

type options struct {
	tracer     trace.Tracer
	registry   prometheus.Registerer
	unaryExtra []grpc.UnaryServerInterceptor
}

// Option customizes the server.
type Option func(*options)

// WithTracer sets the tracer used for trace-id injection. Defaults to a no-op.
func WithTracer(t trace.Tracer) Option {
	return func(o *options) { o.tracer = t }
}

// WithMetrics registers gRPC collectors on reg (e.g. metrics.Metrics.Registry).
func WithMetrics(reg prometheus.Registerer) Option {
	return func(o *options) { o.registry = reg }
}

// WithUnaryInterceptors appends application interceptors, applied after the
// built-in ones (closest to the handler).
func WithUnaryInterceptors(in ...grpc.UnaryServerInterceptor) Option {
	return func(o *options) { o.unaryExtra = append(o.unaryExtra, in...) }
}

// New builds a Server. Register services via ServiceRegistrar before Start.
func New(log *logger.Logger, cfg Config, opts ...Option) *Server {
	o := options{tracer: noop.NewTracerProvider().Tracer("grpc")}
	for _, opt := range opts {
		opt(&o)
	}
	if cfg.ShutdownTimeout == 0 {
		cfg.ShutdownTimeout = 10 * time.Second
	}

	var m *rpcMetrics
	if o.registry != nil {
		m = newRPCMetrics(cfg.MetricsNamespace, o.registry)
	}

	// Order: recovery (outermost) -> trace -> logging -> metrics -> errormap
	// (innermost) -> app interceptors -> handler. logging/metrics observe the
	// already-mapped status code.
	chain := []grpc.UnaryServerInterceptor{
		recoveryUnary(log),
		traceUnary(o.tracer),
		loggingUnary(log),
	}
	if m != nil {
		chain = append(chain, metricsUnary(m))
	}
	chain = append(chain, errorMapUnary())
	chain = append(chain, o.unaryExtra...)

	serverOpts := []grpc.ServerOption{grpc.ChainUnaryInterceptor(chain...)}
	serverOpts = append(serverOpts, buildTuningOptions(cfg)...)

	gs := grpc.NewServer(serverOpts...)

	h := health.NewServer()
	healthpb.RegisterHealthServer(gs, h)
	if cfg.EnableReflection {
		reflection.Register(gs)
	}

	return &Server{log: log, cfg: cfg, gs: gs, health: h}
}

// buildTuningOptions translates the performance fields of Config into gRPC
// server options, leaving anything unset at gRPC's defaults.
func buildTuningOptions(cfg Config) []grpc.ServerOption {
	var opts []grpc.ServerOption

	if cfg.MaxRecvMsgSize > 0 {
		opts = append(opts, grpc.MaxRecvMsgSize(cfg.MaxRecvMsgSize))
	}
	if cfg.MaxSendMsgSize > 0 {
		opts = append(opts, grpc.MaxSendMsgSize(cfg.MaxSendMsgSize))
	}
	if cfg.NumStreamWorkers > 0 {
		opts = append(opts, grpc.NumStreamWorkers(cfg.NumStreamWorkers))
	}
	if cfg.SharedWriteBuffer {
		opts = append(opts, grpc.SharedWriteBuffer(true))
	}

	k := cfg.Keepalive
	if k.MaxConnectionIdle != 0 || k.MaxConnectionAge != 0 || k.Time != 0 || k.Timeout != 0 {
		opts = append(opts, grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle: k.MaxConnectionIdle,
			MaxConnectionAge:  k.MaxConnectionAge,
			Time:              k.Time,
			Timeout:           k.Timeout,
		}))
	}
	if k.MinTime != 0 || k.PermitWithoutStream {
		opts = append(opts, grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             k.MinTime,
			PermitWithoutStream: k.PermitWithoutStream,
		}))
	}

	return opts
}

// ServiceRegistrar returns the registrar used to register gRPC services, e.g.
// widgetv1.RegisterWidgetServiceServer(s.ServiceRegistrar(), handler).
func (s *Server) ServiceRegistrar() grpc.ServiceRegistrar { return s.gs }

// Service is the registration seam a gRPC feature implements so the server need
// not know which concrete services exist or import their generated packages: the
// feature owns the generated RegisterXxxServer call, the server just hands it the
// registrar. It mirrors the REST rest.Handle seam, but as an interface because
// a gRPC service registers as a whole unit rather than per route.
//
//	func (h *Handler) Register(reg grpc.ServiceRegistrar) {
//		widgetv1.RegisterWidgetServiceServer(reg, h)
//	}
type Service interface {
	Register(reg grpc.ServiceRegistrar)
}

// Install registers each Service on the server's registrar. Call it once, before
// Start, with the feature handlers — the equivalent of the REST Install function:
//
//	gs.Install(d.WidgetGRPC(ctx), d.OrderGRPC(ctx))
func (s *Server) Install(svcs ...Service) {
	for _, svc := range svcs {
		svc.Register(s.gs)
	}
}

// Name identifies the server in the supervisor and logs.
func (s *Server) Name() string { return "grpc-server" }

// Start binds a TCP listener on the configured address and serves until Stop.
func (s *Server) Start(ctx context.Context) error {
	lis, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return err
	}
	s.log.Info(ctx, "grpc server listening", "addr", s.cfg.Addr)
	return s.Serve(lis)
}

// Serve serves on the provided listener until Stop. It is useful for tests
// (e.g. a bufconn listener) and for callers that manage their own listener.
func (s *Server) Serve(lis net.Listener) error {
	s.health.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	return s.gs.Serve(lis)
}

// Stop gracefully drains in-flight RPCs, falling back to a hard stop if the
// shutdown timeout elapses.
func (s *Server) Stop(ctx context.Context) error {
	s.health.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	s.log.Info(ctx, "grpc server shutting down")

	done := make(chan struct{})
	go func() {
		s.gs.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(s.cfg.ShutdownTimeout):
		s.gs.Stop()
		return nil
	}
}
