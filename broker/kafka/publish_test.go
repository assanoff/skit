package kafka_test

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"testing"
	"time"

	"github.com/matryer/is"
	kgo "github.com/segmentio/kafka-go"

	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/broker/kafka"
	"github.com/assanoff/skit/internal/brokertest"
)

// TestPublishConsume sends records through a real Kafka one by one, as the
// queue of events does, and reads them back with the consumer: each publish
// takes milliseconds, not the second of the default batch timer, binary data
// survives, and a record carries the key of WithPartitionKey. Another envelope
// works when the publisher and the consumer both take it, and a consumer of
// CloudEvents drops its records.
func TestPublishConsume(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires docker")
	}

	is := is.New(t)
	ctx := t.Context()
	cfg := kafka.Config{Brokers: []string{brokertest.Kafka(ctx, t)}}
	byID := func(m broker.Message) (string, error) { return m.ID, nil }
	pub, err := kafka.NewPublisher(cfg, kafka.PublisherConfig{Source: "test"},
		kafka.WithPartitionKey(byID))
	is.NoErr(err)
	t.Cleanup(func() { _ = pub.Close() })

	msg := func(i int) broker.Message {
		return broker.Message{
			ID: strconv.Itoa(i), Type: "widget.created", Topic: "widgets",
			ContentType: "application/json", Data: fmt.Appendf(nil, `{"id":%d}`, i),
		}
	}
	createTopic(t, pub, msg(0))

	const n = 20
	start := time.Now()
	for i := 1; i <= n; i++ {
		is.NoErr(pub.Publish(ctx, msg(i)))
	}
	elapsed := time.Since(start)
	is.True(elapsed < 2*time.Second) // 1s per record with the default timer
	binary := broker.Message{
		ID: "bin", Type: "widget.deleted", Topic: "widgets",
		ContentType: "application/protobuf", Data: []byte{0x08, 0x02, 0xff},
	}
	is.NoErr(pub.Publish(ctx, binary))

	got := make(chan broker.Message, n+2)
	cons, err := kafka.NewConsumer(slog.New(slog.DiscardHandler), cfg, kafka.ConsumerConfig{
		Topic: "widgets", Group: "test",
	}, func(_ context.Context, m broker.Message) broker.Action {
		got <- m
		return broker.Ack
	})
	is.NoErr(err)
	done := make(chan error, 1)
	go func() { done <- cons.Start(ctx) }()
	t.Cleanup(func() { <-done })

	for range n + 2 {
		select {
		case m := <-got:
			if m.ID != binary.ID {
				continue
			}
			is.Equal(m.Data, binary.Data) // binary data survives as data_base64
			is.Equal(m.ContentType, binary.ContentType)
		case <-time.After(30 * time.Second):
			t.Fatal("records not received")
		}
	}
	is.Equal(keyOf(ctx, t, cfg.Brokers, "widgets", binary.ID), binary.ID)

	rawPub, err := kafka.NewPublisher(cfg, kafka.PublisherConfig{Source: "test"},
		kafka.WithEnvelope(brokertest.Raw{}))
	is.NoErr(err)
	t.Cleanup(func() { _ = rawPub.Close() })
	raw := broker.Message{
		ID: "raw", Type: "widget.raw", Topic: "widgets.raw",
		ContentType: "text/plain", Data: []byte("raw"),
	}
	createTopic(t, rawPub, raw)
	ce := broker.Message{ID: "ce", Type: "widget.created", Topic: "widgets.raw"}
	is.NoErr(pub.Publish(ctx, ce))

	// Each consumer handles the record of its envelope and drops the other.
	rawGot := consume(ctx, t, cfg, "raw", kafka.WithEnvelope(brokertest.Raw{}))
	ceGot := consume(ctx, t, cfg, "ce")
	m := first(t, rawGot)
	is.Equal(m.ID, raw.ID)
	is.Equal(m.Data, raw.Data)
	is.Equal(m.ContentType, raw.ContentType)
	is.Equal(m.Headers["content-type"], "text/plain")
	is.Equal(first(t, ceGot).ID, ce.ID)
}

// consume starts a consumer of the group on the topic widgets.raw and returns
// the channel of the messages it handles.
func consume(
	ctx context.Context, t *testing.T, cfg kafka.Config, group string,
	opts ...kafka.ConsumerOption,
) <-chan broker.Message {
	t.Helper()
	got := make(chan broker.Message, 8)
	h := func(_ context.Context, m broker.Message) broker.Action {
		got <- m
		return broker.Ack
	}
	ccfg := kafka.ConsumerConfig{Topic: "widgets.raw", Group: group}
	cons, err := kafka.NewConsumer(slog.New(slog.DiscardHandler), cfg, ccfg, h, opts...)
	if err != nil {
		t.Fatalf("new consumer %s: %v", group, err)
	}
	done := make(chan error, 1)
	go func() { done <- cons.Start(ctx) }()
	t.Cleanup(func() { <-done })
	return got
}

// first returns the first message of got.
func first(t *testing.T, got <-chan broker.Message) broker.Message {
	t.Helper()
	select {
	case m := <-got:
		return m
	case <-time.After(30 * time.Second):
		t.Fatal("record not received")
	}
	return broker.Message{}
}

// keyOf reads topic from the start up to the record of the message id and
// returns the key of the record.
func keyOf(ctx context.Context, t *testing.T, brokers []string, topic, id string) string {
	t.Helper()
	r := kgo.NewReader(kgo.ReaderConfig{Brokers: brokers, Topic: topic})
	defer func() { _ = r.Close() }()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		rec, err := r.ReadMessage(ctx)
		if err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		m, err := broker.CloudEvents{}.Unmarshal(rec.Value, nil)
		if err == nil && m.ID == id {
			return string(rec.Key)
		}
	}
}

// createTopic publishes m until it goes through: the first write to a new
// topic creates it and fails until the topic exists.
func createTopic(t *testing.T, pub *kafka.Publisher, m broker.Message) {
	t.Helper()
	var err error
	for range 50 {
		if err = pub.Publish(t.Context(), m); err == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("create topic: %v", err)
}
