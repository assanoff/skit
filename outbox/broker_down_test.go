package outbox_test

import (
	"context"
	"log/slog"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/matryer/is"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/broker/rabbitmq"
	"github.com/assanoff/skit/dbtest"
	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/internal/brokertest"
	"github.com/assanoff/skit/outbox"
	"github.com/assanoff/skit/queue"
	"github.com/assanoff/skit/retry"
	"github.com/assanoff/skit/worker"
)

// TestBrokerDown runs a queue of events against a RabbitMQ that stops
// answering for a while. No event goes to the dead letters, and once the
// broker answers again every event arrives. An event arrives twice only after
// a publish that timed out yet reached the broker (at-least-once), so the
// extra deliveries never exceed the failed publishes.
func TestBrokerDown(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires docker")
	}

	is := is.New(t)
	ctx := t.Context()
	log := slog.New(slog.DiscardHandler)
	pg := dbtest.NewPostgres(ctx, t, dbtest.Config{})
	q := queue.NewPG(log, pg.DB)
	is.NoErr(q.EnsureSchema(ctx))

	rmq, rcfg := brokertest.RabbitMQ(ctx, t)
	conn, err := rabbitmq.Dial(rcfg)
	is.NoErr(err)
	t.Cleanup(func() { _ = conn.Close() })
	pub, err := rabbitmq.NewPublisher(conn, rabbitmq.PublisherConfig{Source: "test"})
	is.NoErr(err)
	t.Cleanup(func() { _ = pub.Close() })

	got := newDeliveries()
	cons, err := rabbitmq.NewConsumer(log, conn, rabbitmq.ConsumerConfig{
		Queue:    "widgets.events",
		Exchange: "widgets",
	}, got.handle)
	is.NoErr(err)
	consumed := make(chan error, 1)
	go func() { consumed <- cons.Start(ctx) }()
	t.Cleanup(func() { <-consumed })
	waitBound(t, pub, got)

	reg := queue.NewRegistry()
	enc := queue.WithEncoder(outbox.Envelope())
	is.NoErr(reg.Register[created]("events", "widget.created", enc))
	mux := queue.NewMux()
	is.NoErr(mux.Register("widget.created", outbox.Handler(pub, "widgets")))
	is.NoErr(reg.Check("events", mux))
	const n = 30
	err = dbx.WithinTran(ctx, log, pg.DB, func(tx *dbx.Tx) error {
		s := queue.Bind(tx, q, reg)
		for i := range n {
			if err := s.Schedule(ctx, created{ID: strconv.Itoa(i)}); err != nil {
				return err
			}
		}
		return nil
	})
	is.NoErr(err)

	resume := brokertest.Pause(ctx, t, rmq)
	metrics := prometheus.NewRegistry()
	cfg := outbox.Config("events")
	cfg.Batch = 10
	cfg.Timeout = time.Second
	cfg.Poll = 100 * time.Millisecond
	rs, err := queue.Workers(log, q, cfg, mux.Handle,
		queue.WithBackoff(retry.Backoff{Base: 100 * time.Millisecond, Max: time.Second}),
		queue.WithMetrics(metrics))
	is.NoErr(err)
	group := worker.NewGroup(log, worker.GroupConfig{ShutdownTimeout: 5 * time.Second})
	for _, r := range rs {
		group.Add(r)
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- group.Run(runCtx) }()
	t.Cleanup(func() { cancel(); <-done })

	time.Sleep(4 * time.Second) // the queue keeps trying while the broker hangs
	dead, err := dbx.QueryOne[int](ctx, pg.DB,
		`SELECT count(*) FROM queue_tasks WHERE done_at IS NOT NULL`)
	is.NoErr(err)
	is.Equal(dead, 0) // a broker that hangs dead-letters nothing
	resume()

	deadline := time.Now().Add(60 * time.Second)
	for got.unique() < n && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	is.Equal(got.unique(), n) // every event arrives

	failures := failedHandles(t, metrics)
	is.True(failures > 0)                       // the broker did hang
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

// failedHandles returns the tasks handled with an error, per
// skit_queue_handled_total on reg.
func failedHandles(t *testing.T, reg *prometheus.Registry) float64 {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	var n float64
	for _, mf := range mfs {
		if mf.GetName() != "skit_queue_handled_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			if slices.ContainsFunc(m.GetLabel(), isErrorResult) {
				n += m.GetCounter().GetValue()
			}
		}
	}
	return n
}

// isErrorResult reports whether l is the label result="error".
func isErrorResult(l *dto.LabelPair) bool {
	return l.GetName() == "result" && l.GetValue() == "error"
}
