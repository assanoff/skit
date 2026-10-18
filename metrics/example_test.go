package metrics_test

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/assanoff/skit/metrics"
	"github.com/assanoff/skit/worker"
)

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
