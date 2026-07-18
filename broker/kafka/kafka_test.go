package kafka

import (
	"context"
	"io"
	"testing"

	kgo "github.com/segmentio/kafka-go"

	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/logger"
	"github.com/assanoff/skit/worker"
)

// The consumer is a supervised Runnable.
var _ worker.Runnable = (*Consumer)(nil)

func testLog() *logger.Logger {
	return logger.New(io.Discard, logger.Config{Service: "test", Level: logger.LevelError})
}

func noopHandler(context.Context, broker.Message) broker.Action { return broker.Ack }

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
	if _, err := NewPublisher(Config{Brokers: []string{"b"}}, "", testLog()); err == nil {
		t.Error("expected error when source is empty")
	}
	if _, err := NewPublisher(Config{}, "svc", testLog()); err == nil {
		t.Error("expected error when brokers are empty")
	}
}

func TestHeadersRoundTrip(t *testing.T) {
	if headers(nil) != nil {
		t.Error("no headers should map to nil")
	}
	got := headers([]kgo.Header{{Key: "traceparent", Value: []byte("00-abc")}, {Key: "x", Value: []byte("y")}})
	if got["traceparent"] != "00-abc" || got["x"] != "y" {
		t.Errorf("headers round-trip wrong: %v", got)
	}
}
