package grpcgateway

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/stats"

	"github.com/assanoff/skit/middleware"
	skotel "github.com/assanoff/skit/otel"
)

// HandlerRegistrar registers a generated gRPC-gateway handler onto mux, backed
// by conn. It matches the signature protoc-gen-grpc-gateway emits, e.g.
//
//	widgetv1.RegisterWidgetServiceHandler(ctx, mux, conn)
//
// so the application passes the generated functions directly:
//
//	gw, err := grpcgateway.New(ctx, cfg, widgetv1.RegisterWidgetServiceHandler)
type HandlerRegistrar func(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error

// Config configures the gateway.
type Config struct {
	// Endpoint is the gRPC server target to proxy to (e.g. "localhost:9090" or
	// "dns:///grpc:9090"). Required unless ClientConn is supplied.
	Endpoint string
	// ClientConn, when set, is used as-is and Endpoint/DialOptions are ignored;
	// the caller owns its lifecycle (close it via your closer/DI). When nil, New
	// dials Endpoint and the returned Gateway owns and closes that connection.
	ClientConn *grpc.ClientConn
	// DialOptions override the options used to dial Endpoint. The default is a
	// single insecure transport credential — the gateway and the gRPC server are
	// expected to be co-located behind the same trust boundary. New adds the
	// client span handler (ClientHandler) to them.
	DialOptions []grpc.DialOption
	// MuxOptions are forwarded to runtime.NewServeMux (custom marshaler, error
	// handler, header matchers, ...).
	MuxOptions []runtime.ServeMuxOption
	// TracerProvider records the client spans of the calls to the gRPC server;
	// nil → the global provider.
	TracerProvider trace.TracerProvider
}

// Gateway is the assembled gRPC-gateway: an http.Handler (the runtime.ServeMux)
// plus the gRPC client connection it proxies through. Because it embeds
// http.Handler, run it via httpserver.New(cfg, gw) like any other handler; call
// Close on shutdown to release a dialed connection.
type Gateway struct {
	http.Handler

	conn    *grpc.ClientConn
	ownConn bool
}

// New dials the gRPC endpoint (or uses cfg.ClientConn), builds a runtime
// ServeMux, applies every registrar in order, and returns the resulting
// Gateway. If a registrar fails, a connection dialed by New is closed before
// returning the error.
func New(ctx context.Context, cfg Config, register ...HandlerRegistrar) (*Gateway, error) {
	conn := cfg.ClientConn
	ownConn := false
	if conn == nil {
		opts := cfg.DialOptions
		if opts == nil {
			opts = []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
		}
		opts = append(slices.Clone(opts), grpc.WithStatsHandler(ClientHandler(cfg.TracerProvider)))
		c, err := grpc.NewClient(cfg.Endpoint, opts...)
		if err != nil {
			return nil, fmt.Errorf("grpcgateway: dial %q: %w", cfg.Endpoint, err)
		}
		conn = c
		ownConn = true
	}

	muxOpts := append([]runtime.ServeMuxOption{runtime.WithMiddlewares(nameSpan)},
		cfg.MuxOptions...)
	mux := runtime.NewServeMux(muxOpts...)
	for _, reg := range register {
		if reg == nil {
			continue
		}
		if err := reg(ctx, mux, conn); err != nil {
			if ownConn {
				_ = conn.Close()
			}
			return nil, fmt.Errorf("grpcgateway: register handler: %w", err)
		}
	}

	return &Gateway{Handler: mux, conn: conn, ownConn: ownConn}, nil
}

// Close releases the gRPC client connection when New dialed it; it is a no-op
// when the caller supplied cfg.ClientConn (that connection's lifecycle stays
// with the caller).
func (g *Gateway) Close() error {
	if g.ownConn && g.conn != nil {
		if err := g.conn.Close(); err != nil {
			return fmt.Errorf("grpcgateway: close client conn: %w", err)
		}
	}
	return nil
}

// ClientHandler returns the stats handler that opens a client span per call
// the gateway makes, continuing the server span of the HTTP request. New adds
// it to the connection it dials; dial a Config.ClientConn with it as well:
//
//	grpc.NewClient(target, grpc.WithStatsHandler(grpcgateway.ClientHandler(tp)))
//
// A nil tp means the global provider.
func ClientHandler(tp trace.TracerProvider) stats.Handler {
	return otelgrpc.NewClientHandler(
		otelgrpc.WithTracerProvider(cmp.Or(tp, otel.GetTracerProvider())),
		otelgrpc.WithPropagators(skotel.Propagator()),
		// RPC metrics come from package metrics; otelgrpc records only spans.
		otelgrpc.WithMeterProvider(noop.NewMeterProvider()),
	)
}

// nameSpan names the server span of the request after the gateway route the
// mux matched (middleware.SetRoute), not after the prefix the gateway is
// mounted on.
func nameSpan(next runtime.HandlerFunc) runtime.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, params map[string]string) {
		if p, ok := runtime.HTTPPattern(r.Context()); ok {
			middleware.SetRoute(r, route(p))
		}
		next(w, r, params)
	}
}

// route renders a gateway pattern as the template of its google.api.http
// rule: "/v1/widgets/{id=*}" → "/v1/widgets/{id}". A variable bound to a
// longer path, such as "{name=shelves/*}", keeps its binding.
func route(p runtime.Pattern) string {
	return strings.ReplaceAll(p.String(), "=*}", "}")
}
