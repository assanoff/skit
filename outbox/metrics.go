package outbox

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/assanoff/skit/logger"
	"github.com/assanoff/skit/metrics"
)

// metricsNamespace/Subsystem name every collector this package registers, so
// outbox metrics never collide with the application's own or another
// subsystem's. Series are exposed as skit_outbox_*.
const (
	metricsNamespace = "skit"
	metricsSubsystem = "outbox"
)

// Metrics are the outbox's own throughput counters, owned by this package and
// registered on the shared registry passed to NewMetrics. Wire it into
// RelayConfig/SweeperConfig/CleanerConfig to record relay activity; a nil
// *Metrics is a safe no-op, so metrics stay optional.
//
// Backlog gauges (depth, age) are separate — they need a query, so they live in
// the BacklogCollector rather than being incremented inline here.
type Metrics struct {
	published     prometheus.Counter
	publishFailed prometheus.Counter
	publishDur    prometheus.Histogram
	swept         prometheus.Counter
	cleaned       prometheus.Counter
}

// NewMetrics builds the outbox counters and registers them on reg (idempotently,
// via metrics.Register). reg may be nil to disable them.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	return &Metrics{
		published: metrics.Register(reg, prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: metricsSubsystem,
			Name: "relay_published_total",
			Help: "Events the relay published to the broker successfully.",
		})),
		publishFailed: metrics.Register(reg, prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: metricsSubsystem,
			Name: "relay_publish_failures_total",
			Help: "Relay publish attempts that returned an error.",
		})),
		publishDur: metrics.Register(reg, prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: metricsNamespace, Subsystem: metricsSubsystem,
			Name:    "relay_publish_duration_seconds",
			Help:    "Latency of a single relay publish call.",
			Buckets: prometheus.DefBuckets,
		})),
		swept: metrics.Register(reg, prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: metricsSubsystem,
			Name: "sweeper_reclaimed_total",
			Help: "Expired leases the sweeper returned to pending.",
		})),
		cleaned: metrics.Register(reg, prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: metricsSubsystem,
			Name: "cleaner_deleted_total",
			Help: "Terminal rows the cleaner deleted past retention.",
		})),
	}
}

func (m *Metrics) observePublish(d time.Duration, err error) {
	if m == nil {
		return
	}
	if err != nil {
		m.publishFailed.Inc()
		return
	}
	m.published.Inc()
	m.publishDur.Observe(d.Seconds())
}

func (m *Metrics) addSwept(n int64) {
	if m == nil || n <= 0 {
		return
	}
	m.swept.Add(float64(n))
}

func (m *Metrics) addCleaned(n int64) {
	if m == nil || n <= 0 {
		return
	}
	m.cleaned.Add(float64(n))
}

// Stats is a point-in-time snapshot of the outbox table used for backlog
// metrics: how much work is waiting and how old the oldest waiting event is.
type Stats struct {
	Pending          int64
	InFlight         int64
	Failed           int64
	OldestPendingAge time.Duration // age of the oldest pending row; 0 when none
}

// StatsReader reports outbox backlog stats. *PG implements it; it is a separate
// capability from Store so the core port stays minimal and backlog metrics stay
// opt-in.
type StatsReader interface {
	Stats(ctx context.Context, now time.Time) (Stats, error)
}

// BacklogCollector is a prometheus.Collector that, on each scrape, reads the
// outbox backlog via a StatsReader and reports it as gauges:
//
//	skit_outbox_pending             — events awaiting the relay
//	skit_outbox_in_flight           — events leased, publish in progress
//	skit_outbox_failed              — terminal failures awaiting attention
//	skit_outbox_oldest_pending_seconds — age of the oldest pending event
//
// It queries the store on every scrape, so keep the scrape interval sane across
// replicas. A query error is logged and that scrape emits no outbox gauges
// (rather than stale values). Register it on the shared registry like any
// collector.
type BacklogCollector struct {
	sr      StatsReader
	log     *logger.Logger
	timeout time.Duration

	pending  *prometheus.Desc
	inFlight *prometheus.Desc
	failed   *prometheus.Desc
	oldest   *prometheus.Desc
}

// BacklogOption customizes a BacklogCollector.
type BacklogOption func(*BacklogCollector)

// WithScrapeTimeout bounds the stats query run on each scrape (default 2s).
func WithScrapeTimeout(d time.Duration) BacklogOption {
	return func(c *BacklogCollector) {
		if d > 0 {
			c.timeout = d
		}
	}
}

// NewBacklogCollector builds a backlog collector over sr. log may be nil.
func NewBacklogCollector(sr StatsReader, log *logger.Logger, opts ...BacklogOption) *BacklogCollector {
	desc := func(name, help string) *prometheus.Desc {
		return prometheus.NewDesc(
			prometheus.BuildFQName(metricsNamespace, metricsSubsystem, name), help, nil, nil,
		)
	}
	c := &BacklogCollector{
		sr:       sr,
		log:      log,
		timeout:  2 * time.Second,
		pending:  desc("pending", "Events awaiting the relay (status=pending)."),
		inFlight: desc("in_flight", "Events leased by a relay, publish in progress."),
		failed:   desc("failed", "Events in the terminal failed state."),
		oldest:   desc("oldest_pending_seconds", "Age in seconds of the oldest pending event (0 when none)."),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Describe implements prometheus.Collector.
func (c *BacklogCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.pending
	ch <- c.inFlight
	ch <- c.failed
	ch <- c.oldest
}

// Collect implements prometheus.Collector: it queries the backlog and emits the
// gauges. On a query error it logs and emits nothing for this scrape.
func (c *BacklogCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	st, err := c.sr.Stats(ctx, time.Now().UTC())
	if err != nil {
		if c.log != nil {
			c.log.Error(ctx, "outbox backlog collect failed", "err", err)
		}
		return
	}
	ch <- prometheus.MustNewConstMetric(c.pending, prometheus.GaugeValue, float64(st.Pending))
	ch <- prometheus.MustNewConstMetric(c.inFlight, prometheus.GaugeValue, float64(st.InFlight))
	ch <- prometheus.MustNewConstMetric(c.failed, prometheus.GaugeValue, float64(st.Failed))
	ch <- prometheus.MustNewConstMetric(c.oldest, prometheus.GaugeValue, st.OldestPendingAge.Seconds())
}
