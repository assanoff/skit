package metrics_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/assanoff/skit/metrics"
	"github.com/assanoff/skit/rest/router"
	"github.com/assanoff/skit/worker"
)

// The process builds one Metrics. Its Middleware records the HTTP server
// metrics of the group of business routes, labeled by the route template;
// Handler serves every metric of the registry to the scraper.
func Example() {
	m := metrics.New()

	r := router.New()
	r.Handle("GET /metrics", m.Handler())
	api := r.With(m.Middleware()) // after middleware.Trace in a service
	api.HandleFunc("GET /widgets/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/widgets/42", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/widgets/7", nil))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for line := range strings.Lines(w.Body.String()) {
		if strings.HasPrefix(line, "skit_http_server_requests_total{") {
			fmt.Print(line)
		}
	}
	// Output:
	// skit_http_server_requests_total{http_request_method="GET",http_response_status_code="200",http_route="/widgets/{id}"} 2
}

// orderMetrics are the metrics of a domain package, core/order/metrics.go in a
// service.
type orderMetrics struct {
	created *prometheus.CounterVec
	pending prometheus.Gauge
}

// newOrderMetrics builds the set once, on the registry the core gets in its
// Config. With a nil reg the collectors work but are not registered, so neither
// the core nor its tests check for nil.
func newOrderMetrics(reg prometheus.Registerer) orderMetrics {
	f := promauto.With(reg)
	return orderMetrics{
		created: f.NewCounterVec(prometheus.CounterOpts{
			Name: "shop_order_created_total", Help: "Orders created.",
		}, []string{"channel"}),
		pending: f.NewGauge(prometheus.GaugeOpts{
			Name: "shop_order_pending", Help: "Orders awaiting payment.",
		}),
	}
}

// A domain package records on the registry of the process; a test reads the
// value back with testutil.
func Example_businessMetrics() {
	m := metrics.New()
	om := newOrderMetrics(m)

	om.created.WithLabelValues("web").Inc()
	om.created.WithLabelValues("web").Inc()

	fmt.Println(testutil.ToFloat64(om.created.WithLabelValues("web")))
	// Output: 2
}

// A cheap read from memory is a GaugeFunc: it runs on every scrape.
func Example_gaugeFunc() {
	m := metrics.New()
	carts := []string{"cart-1", "cart-2"}

	open := promauto.With(m).NewGaugeFunc(prometheus.GaugeOpts{
		Name: "shop_cart_open", Help: "Open carts in memory.",
	}, func() float64 { return float64(len(carts)) })

	fmt.Println(testutil.ToFloat64(open))
	// Output: 2
}

// An expensive read runs in a worker.Loop that sets the gauge. The Loop goes
// into the worker group and records its skit_worker_* on the same registry,
// which tells how fresh the gauge is.
func Example_backgroundGauge() {
	m := metrics.New()
	om := newOrderMetrics(m)
	countPending := func(context.Context) (int, error) { return 3, nil } // an SQL count

	loop := worker.NewLoop(slog.New(slog.DiscardHandler), worker.LoopConfig{
		Name: "order-pending", Interval: time.Minute, Metrics: m,
	}, func(ctx context.Context) error {
		n, err := countPending(ctx)
		if err != nil {
			return err
		}
		om.pending.Set(float64(n))
		return nil
	})

	fmt.Println(loop.Name()) // group.Add(loop)
	// Output: order-pending
}
