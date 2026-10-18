package httpclient

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/assanoff/skit/httpmw"
	"github.com/assanoff/skit/internal/prom"
	"github.com/assanoff/skit/middleware"
)

// The HTTP client metrics are named skit_http_client_*. The labels of the
// request are named after the attributes of the HTTP semantic conventions, as
// the client span names them.
const (
	metricsNamespace = "skit"
	metricsSubsystem = "http_client"

	labelClient    = "client"
	labelMethod    = "http_request_method"
	labelStatus    = "http_response_status_code"
	labelErrorType = "error_type"
)

// clientMetrics are the metrics of the requests of one client.
type clientMetrics struct {
	requests *prometheus.CounterVec // bound to the client
	duration prometheus.Observer
}

// newClientMetrics builds the metrics of the client name and registers them on
// reg. The clients on one registry share the metrics, each under its own name.
func newClientMetrics(reg prometheus.Registerer, name string) *clientMetrics {
	requests := prom.MustRegister(reg, prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace, Subsystem: metricsSubsystem,
		Name: "requests_total",
		Help: "Attempts of HTTP requests, by client, method, status code and error type.",
	}, []string{labelClient, labelMethod, labelStatus, labelErrorType}))
	duration := prom.MustRegister(reg, prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricsNamespace, Subsystem: metricsSubsystem,
		Name:    "request_duration_seconds",
		Help:    "Duration of attempts of HTTP requests, by client.",
		Buckets: prom.DurationBuckets(),
	}, []string{labelClient}))
	return &clientMetrics{
		requests: requests.MustCurryWith(prometheus.Labels{labelClient: name}),
		duration: duration.WithLabelValues(name),
	}
}

// metricsTransport records every attempt that passes through it. Put it below
// retry and the client span, so it records each attempt with the span of the
// attempt as the exemplar.
func metricsTransport(m *clientMetrics) httpmw.Middleware {
	return func(next http.RoundTripper) http.RoundTripper {
		return recordingTransport{next: next, m: m}
	}
}

// recordingTransport records the attempts it sends through next in m.
type recordingTransport struct {
	next http.RoundTripper
	m    *clientMetrics
}

// RoundTrip implements http.RoundTripper.
func (t recordingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := t.next.RoundTrip(r)
	code, errType := outcome(resp, err)
	t.m.requests.WithLabelValues(middleware.Method(r), code, errType).Inc()
	prom.ObserveDuration(r.Context(), t.m.duration, time.Since(start))
	return resp, err
}

// outcome is the status code of a response, or the error type of a request
// that got none.
func outcome(resp *http.Response, err error) (code, errType string) {
	if err != nil {
		return "", errorType(err)
	}
	return strconv.Itoa(resp.StatusCode), ""
}

// errorType is the class of a transport error: "canceled", "timeout", or
// "_OTHER" for the rest, as error.type of the semantic conventions records an
// error it has no name for.
func errorType(err error) string {
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if ne, ok := errors.AsType[net.Error](err); ok && ne.Timeout() {
		return "timeout"
	}
	return "_OTHER"
}
