package grpcserver

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

// rpcMetrics holds the gRPC server collectors. They are registered on a registry
// supplied by the caller (typically the shared metrics.Metrics.Registry), so
// gRPC and HTTP metrics live in one place without the metrics package depending
// on gRPC.
type rpcMetrics struct {
	total  *prometheus.CounterVec
	millis *prometheus.HistogramVec
}

func newRPCMetrics(namespace string, reg prometheus.Registerer) *rpcMetrics {
	if namespace == "" {
		namespace = "grpc"
	}
	total := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "requests_total",
		Help:      "Total number of gRPC requests.",
	}, []string{"method", "code"})

	millis := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "request_duration_ms",
		Help:      "gRPC request latency in milliseconds.",
		Buckets:   []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500},
	}, []string{"method", "code"})

	// Register tolerantly: if the caller registered equivalent collectors
	// already, reuse them instead of panicking.
	total = registerCounterVec(reg, total)
	millis = registerHistogramVec(reg, millis)

	return &rpcMetrics{total: total, millis: millis}
}

func (m *rpcMetrics) observe(method, code string, ms float64) {
	if m == nil {
		return
	}
	labels := prometheus.Labels{"method": method, "code": code}
	m.total.With(labels).Inc()
	m.millis.With(labels).Observe(ms)
}

func registerCounterVec(reg prometheus.Registerer, c *prometheus.CounterVec) *prometheus.CounterVec {
	if err := reg.Register(c); err != nil {
		var are prometheus.AlreadyRegisteredError
		if errors.As(err, &are) {
			if existing, ok := are.ExistingCollector.(*prometheus.CounterVec); ok {
				return existing
			}
		}
	}
	return c
}

func registerHistogramVec(reg prometheus.Registerer, h *prometheus.HistogramVec) *prometheus.HistogramVec {
	if err := reg.Register(h); err != nil {
		var are prometheus.AlreadyRegisteredError
		if errors.As(err, &are) {
			if existing, ok := are.ExistingCollector.(*prometheus.HistogramVec); ok {
				return existing
			}
		}
	}
	return h
}
