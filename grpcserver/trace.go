package grpcserver

import (
	"cmp"
	"strings"
	"sync/atomic"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/stats"

	skotel "github.com/assanoff/skit/otel"
)

// methodSet holds the full names ("/pkg.Service/Method") of the methods that
// get spans. Serve fills it after the services are registered.
type methodSet struct {
	names atomic.Pointer[map[string]struct{}]
}

// load collects the registered methods of gs, leaving out the technical
// services: health and reflection.
func (m *methodSet) load(gs *grpc.Server) {
	names := map[string]struct{}{}
	for svc, info := range gs.GetServiceInfo() {
		if strings.HasPrefix(svc, "grpc.health.") || strings.HasPrefix(svc, "grpc.reflection.") {
			continue
		}
		for _, method := range info.Methods {
			names["/"+svc+"/"+method.Name] = struct{}{}
		}
	}
	m.names.Store(&names)
}

func (m *methodSet) has(fullMethod string) bool {
	names := m.names.Load()
	if names == nil {
		return false
	}
	_, ok := (*names)[fullMethod]
	return ok
}

// serverSpans returns the stats handler that opens a server span per RPC. It
// continues the W3C trace context of the request metadata. Health checks,
// reflection and methods the server does not serve get no span, so a scanner
// cannot fill the trace UI with spans of made-up names. RPC metrics come from
// WithMetrics, so otelgrpc records none.
func serverSpans(tp trace.TracerProvider, methods *methodSet) stats.Handler {
	return otelgrpc.NewServerHandler(
		otelgrpc.WithTracerProvider(cmp.Or(tp, otel.GetTracerProvider())),
		otelgrpc.WithPropagators(skotel.Propagator()),
		otelgrpc.WithMeterProvider(noop.NewMeterProvider()),
		otelgrpc.WithFilter(func(info *stats.RPCTagInfo) bool {
			return methods.has(info.FullMethodName)
		}),
	)
}
