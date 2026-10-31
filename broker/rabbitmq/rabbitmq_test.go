package rabbitmq_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/matryer/is"
	rmq "github.com/wagslane/go-rabbitmq"

	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/broker/rabbitmq"
	"github.com/assanoff/skit/internal/brokertest"
)

func TestToAction(t *testing.T) {
	is := is.New(t)
	is.Equal(rabbitmq.ToAction(broker.Ack), rmq.Ack)
	is.Equal(rabbitmq.ToAction(broker.Requeue), rmq.NackRequeue)
	is.Equal(rabbitmq.ToAction(broker.Discard), rmq.NackDiscard)
}

// TestPublishConsume sends JSON and binary data through a real RabbitMQ: the
// consumer gets every message it binds with its data, type and topic. The
// routing key is the type, or what WithRoutingKey derives; a message no
// binding matches is dropped.
func TestPublishConsume(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires docker")
	}

	is := is.New(t)
	ctx := t.Context()
	_, url := brokertest.RabbitMQ(ctx, t)
	conn, err := rabbitmq.Dial(rabbitmq.Config{URL: url})
	is.NoErr(err)
	t.Cleanup(func() { _ = conn.Close() })

	got := make(chan broker.Message, 64)
	cons, err := rabbitmq.NewConsumer(conn, slog.New(slog.DiscardHandler), rabbitmq.ConsumerConfig{
		Queue:       "widgets.test",
		Exchange:    "widgets",
		RoutingKeys: []string{"ping", "widget.created", "custom"},
	}, func(_ context.Context, m broker.Message) broker.Action {
		got <- m
		return broker.Ack
	})
	is.NoErr(err)
	done := make(chan error, 1)
	go func() { done <- cons.Start(ctx) }()
	t.Cleanup(func() { <-done })

	pub, err := rabbitmq.NewPublisher(conn, rabbitmq.PublisherConfig{Source: "test"})
	is.NoErr(err)
	t.Cleanup(func() { _ = pub.Close() })
	waitBound(t, pub, got)
	customKey := func(broker.Message) string { return "custom" }
	custom, err := rabbitmq.NewPublisher(conn, rabbitmq.PublisherConfig{Source: "test"},
		rabbitmq.WithRoutingKey(customKey))
	is.NoErr(err)
	t.Cleanup(func() { _ = custom.Close() })

	created := broker.Message{
		ID: "1", Type: "widget.created", Topic: "widgets",
		ContentType: "application/json", Data: []byte(`{"id":1}`),
	}
	deleted := broker.Message{
		ID: "2", Type: "widget.deleted", Topic: "widgets",
		ContentType: "application/protobuf", Data: []byte{0x08, 0x02, 0xff},
	}
	is.NoErr(pub.Publish(ctx, created))
	is.NoErr(pub.Publish(ctx, deleted)) // no binding for widget.deleted: dropped
	is.NoErr(custom.Publish(ctx, deleted))

	// One queue, one consumer: an unbound message would come before the next.
	for _, want := range []broker.Message{created, deleted} {
		m := receive(t, got)
		is.Equal(m.ID, want.ID)
		is.Equal(m.Type, want.Type)
		is.Equal(m.Source, "test")
		is.Equal(m.ContentType, want.ContentType)
		is.Equal(m.Data, want.Data) // binary data survives as data_base64
		is.Equal(m.Topic, want.Topic)
	}

	emptyKey := func(broker.Message) string { return "" }
	empty, err := rabbitmq.NewPublisher(conn, rabbitmq.PublisherConfig{Source: "test"},
		rabbitmq.WithRoutingKey(emptyKey))
	is.NoErr(err)
	t.Cleanup(func() { _ = empty.Close() })
	err = empty.Publish(ctx, created)
	is.True(err != nil)               // no routing key
	is.True(!broker.IsPermanent(err)) // the function is wrong, not the message
}

// waitBound publishes pings until one comes back: the consumer declares its
// queue and binding when it starts, and until then a message is unroutable
// and dropped.
func waitBound(t *testing.T, pub *rabbitmq.Publisher, got <-chan broker.Message) {
	t.Helper()
	ping := broker.Message{Type: "ping", Topic: "widgets"}
	for range 50 {
		// Before the exchange exists the broker nacks the ping.
		_ = pub.Publish(t.Context(), ping)
		select {
		case <-got:
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
	t.Fatal("consumer not bound")
}

// receive returns the next message that is not a ping.
func receive(t *testing.T, got <-chan broker.Message) broker.Message {
	t.Helper()
	for {
		select {
		case m := <-got:
			if m.Type != "ping" {
				return m
			}
		case <-time.After(10 * time.Second):
			t.Fatal("message not received")
		}
	}
}
