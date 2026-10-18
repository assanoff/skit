package kafka

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	kgo "github.com/segmentio/kafka-go"

	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/worker"
)

// The consumer is a supervised Runnable.
var _ worker.Runnable = (*Consumer)(nil)

func testLog() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func noopHandler(context.Context, broker.Message) broker.Action {
	return broker.Ack
}

func TestNewConsumerValidates(t *testing.T) {
	cases := []struct {
		name string
		cfg  ConsumerConfig
	}{
		{"no brokers", ConsumerConfig{Topic: "t", GroupID: "g"}},
		{"no topic", ConsumerConfig{Brokers: []string{"b"}, GroupID: "g"}},
		{"no group", ConsumerConfig{Brokers: []string{"b"}, Topic: "t"}},
	}
	for _, tc := range cases {
		if _, err := NewConsumer(tc.cfg, testLog(), noopHandler); err == nil {
			t.Errorf("%s: expected a validation error", tc.name)
		}
	}
}

func TestNewMapsSubscriptionAndDefaults(t *testing.T) {
	c, err := New(Config{Brokers: []string{"localhost:9092"}}, testLog(), broker.Subscription{
		Name:        "orders",
		Topic:       "orders.topic",
		Group:       "svc",
		Concurrency: 3,
	}, noopHandler)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.cfg.Topic != "orders.topic" || c.cfg.GroupID != "svc" || c.cfg.Concurrency != 3 || c.cfg.Name != "orders" {
		t.Fatalf("subscription not mapped: %+v", c.cfg)
	}
	if c.cfg.MaxRetries != DefaultMaxRetries || c.cfg.RetryBackoff != DefaultRetryBackoff {
		t.Errorf("defaults not applied: %+v", c.cfg)
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	c, err := New(Config{Brokers: []string{"b"}}, testLog(), broker.Subscription{Topic: "t", Group: "g"}, noopHandler)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.cfg.Concurrency != 1 {
		t.Errorf("default concurrency = %d, want 1", c.cfg.Concurrency)
	}
	if c.cfg.Name != "consumer" {
		t.Errorf("default name = %q, want consumer", c.cfg.Name)
	}
}

func TestNewPublisherValidates(t *testing.T) {
	if _, err := NewPublisher(Config{Brokers: []string{"b"}}, PublisherConfig{}); err == nil {
		t.Error("expected error when source is empty")
	}
	if _, err := NewPublisher(Config{}, PublisherConfig{Source: "svc"}); err == nil {
		t.Error("expected error when brokers are empty")
	}
}

func TestHeadersRoundTrip(t *testing.T) {
	if Headers(nil) != nil {
		t.Error("no headers should map to nil")
	}
	got := Headers([]kgo.Header{{Key: "traceparent", Value: []byte("00-abc")}, {Key: "x", Value: []byte("y")}})
	if got["traceparent"] != "00-abc" || got["x"] != "y" {
		t.Errorf("headers round-trip wrong: %v", got)
	}
}

// countHandler counts the records it handles.
type countHandler struct {
	slog.Handler

	n *atomic.Int64
}

func (h countHandler) Handle(context.Context, slog.Record) error {
	h.n.Add(1)
	return nil
}

// TestConsumeReturnsWhenReaderClosed checks that consume returns once Stop has
// closed the reader, even while ctx is still live: FetchMessage then reports
// io.EOF, which is a shutdown, not a fetch error to log and retry.
func TestConsumeReturnsWhenReaderClosed(t *testing.T) {
	var logged atomic.Int64
	log := slog.New(countHandler{Handler: slog.DiscardHandler, n: &logged})
	c, err := NewConsumer(ConsumerConfig{Brokers: []string{"127.0.0.1:1"}, Topic: "t", GroupID: "g"}, log, noopHandler)
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}
	r := kgo.NewReader(kgo.ReaderConfig{Brokers: []string{"127.0.0.1:1"}, Topic: "t"})
	if err := r.Close(); err != nil {
		t.Fatalf("close reader: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		c.consume(t.Context(), r)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("consume kept fetching from a closed reader")
	}
	if n := logged.Load(); n != 0 {
		t.Errorf("logged %d records for a closed reader, want 0", n)
	}
}
