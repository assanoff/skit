package grpcserver

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/assanoff/skit/internal/prom"
)

// The gRPC server metrics are named skit_grpc_server_*.
const (
	metricsNamespace = "skit"
	metricsSubsystem = "grpc_server"
)

// rpcMetrics are the gRPC server metrics the metrics interceptors record for
// the methods in methods.
type rpcMetrics struct {
	methods  *methodSet
	handled  *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inFlight prometheus.Gauge
}

// newRPCMetrics builds the gRPC server metrics and registers them on reg. A
// second server on the same reg shares them.
func newRPCMetrics(reg prometheus.Registerer, methods *methodSet) *rpcMetrics {
	return &rpcMetrics{
		methods: methods,
		handled: prom.MustRegister(reg, prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: metricsSubsystem,
			Name: "handled_total",
			Help: "RPCs completed, by method and status code.",
		}, []string{"method", "code"})),
		duration: prom.MustRegister(reg, prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace, Subsystem: metricsSubsystem,
			Name:    "handling_seconds",
			Help:    "Duration of RPCs, by method.",
			Buckets: prom.DurationBuckets(),
		}, []string{"method"})),
		inFlight: prom.MustRegister(reg, prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace, Subsystem: metricsSubsystem,
			Name: "in_flight",
			Help: "RPCs being handled.",
		})),
	}
}

// record counts an RPC of fullMethod that ended with err after d. The method
// is recorded without its leading "/", the status code by its canonical name
// ("NOT_FOUND"), as the RPC semantic conventions name them.
func (m *rpcMetrics) record(ctx context.Context, fullMethod string, err error, d time.Duration) {
	method := strings.TrimPrefix(fullMethod, "/")
	m.handled.WithLabelValues(method, codeName(status.Code(err))).Inc()
	prom.ObserveDuration(ctx, m.duration.WithLabelValues(method), d)
}

// codeName is the canonical name of c ("NOT_FOUND"), or its number for a code
// gRPC does not define.
func codeName(c codes.Code) string {
	// google.rpc.Code numbers the codes as gRPC does, up to Unauthenticated;
	// above it the int32 enum has no name, and a large uint32 would overflow.
	if c > codes.Unauthenticated {
		return strconv.FormatUint(uint64(c), 10)
	}
	return code.Code(c).String()
}
