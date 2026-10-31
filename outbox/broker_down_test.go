package outbox_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/matryer/is"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/broker/rabbitmq"
	"github.com/assanoff/skit/dbtest"
	"github.com/assanoff/skit/internal/brokertest"
	"github.com/assanoff/skit/outbox"
	"github.com/assanoff/skit/worker"
)

// TestRelayBrokerDown runs the relay against a RabbitMQ that stops answering
// for a while. No event becomes failed, no lease expires under a batch the
// broker holds up, and once the broker answers again every event arrives. An
// event arrives twice only after a publish that timed out yet reached the
// broker (at-least-once), so the extra deliveries never exceed the failed
// publishes.
func TestRelayBrokerDown(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires docker")
	}

	is := is.New(t)
	ctx := t.Context()
	log := slog.New(slog.DiscardHandler)
	pg := dbtest.NewPostgres(ctx, t, dbtest.Config{})
	store := outbox.NewPG(log, pg.DB, outbox.Options{})
	is.NoErr(store.EnsureSchema(ctx))

	rmq, url := brokertest.RabbitMQ(ctx, t)
	conn, err := rabbitmq.Dial(rabbitmq.Config{URL: url})
	is.NoErr(err)
	t.Cleanup(func() { _ = conn.Close() })
	pub, err := rabbitmq.NewPublisher(conn, rabbitmq.PublisherConfig{Source: "test"})
	is.NoErr(err)
	t.Cleanup(func() { _ = pub.Close() })

	got := newDeliveries()
	cons, err := rabbitmq.NewConsumer(conn, log, rabbitmq.ConsumerConfig{
		Queue:    "widgets.relay",
		Exchange: "widgets",
	}, got.handle)
	is.NoErr(err)
	consumed := make(chan error, 1)
	go func() { consumed <- cons.Start(ctx) }()
	t.Cleanup(func() { <-consumed })
	waitBound(t, pub, got)

	const n = 30
	for range n {
		ev, err := outbox.NewEvent("widget.created", "widgets", "application/json", []byte(`{}`), nil)
		is.NoErr(err)
		is.NoErr(store.Insert(ctx, ev))
	}

	resume := brokertest.Pause(ctx, t, rmq)
	reg := prometheus.NewRegistry()
	group := worker.NewGroup(log, worker.GroupConfig{ShutdownTimeout: 5 * time.Second})
	group.Add(outbox.NewRelay(log, store, pub, outbox.RelayConfig{
		PollInterval:   100 * time.Millisecond,
		BatchSize:      10,
		PublishTimeout: time.Second,
		Metrics:        reg,
	}))
	// Without the fast fail a batch held up by the broker outlives its lease
	// (10 × 1s against 2s), the sweeper hands it out again, and events go
	// twice.
	group.Add(outbox.NewSweeper(log, store, outbox.SweeperConfig{
		Interval:     200 * time.Millisecond,
		LeaseTimeout: 2 * time.Second,
		Metrics:      reg,
	}))
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- group.Run(runCtx) }()
	t.Cleanup(func() { cancel(); <-done })

	time.Sleep(4 * time.Second) // the relay keeps trying while the broker hangs
	stats, err := store.Stats(ctx, time.Now().UTC())
	is.NoErr(err)
	is.Equal(stats.Failed, int64(0)) // a broker that hangs fails nothing
	resume()

	deadline := time.Now().Add(60 * time.Second)
	for got.unique() < n && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	is.Equal(got.unique(), n) // every event arrives

	failures := counter(t, reg, "skit_outbox_relay_publish_failures_total")
	is.True(failures > 0) // the broker did hang
	is.Equal(counter(t, reg, "skit_outbox_sweeper_reclaimed_total"), 0.0)
	is.True(float64(got.total()-n) <= failures) // no extra copies beyond the timeouts
}

// deliveries counts the events a consumer gets by id; a ping only signals.
type deliveries struct {
	mu    sync.Mutex
	byID  map[string]int
	pings chan struct{}
}

func newDeliveries() *deliveries {
	return &deliveries{byID: map[string]int{}, pings: make(chan struct{}, 64)}
}

func (d *deliveries) handle(_ context.Context, m broker.Message) broker.Action {
	if m.Type == "ping" {
		d.pings <- struct{}{}
		return broker.Ack
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.byID[m.ID]++
	return broker.Ack
}

func (d *deliveries) unique() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.byID)
}

func (d *deliveries) total() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, c := range d.byID {
		n += c
	}
	return n
}

// waitBound publishes pings until one comes back: the consumer declares its
// queue and binding when it starts, and until then a message is unroutable
// and dropped.
func waitBound(t *testing.T, pub broker.Publisher, d *deliveries) {
	t.Helper()
	ping := broker.Message{Type: "ping", Topic: "widgets"}
	for range 50 {
		// Before the exchange exists the broker nacks the ping.
		_ = pub.Publish(t.Context(), ping)
		select {
		case <-d.pings:
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
	t.Fatal("consumer not bound")
}

// counter returns the value of the counter name on reg.
func counter(t *testing.T, reg *prometheus.Registry, name string) float64 {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == name {
			return mf.GetMetric()[0].GetCounter().GetValue()
		}
	}
	t.Fatalf("no metric %s", name)
	return 0
}
