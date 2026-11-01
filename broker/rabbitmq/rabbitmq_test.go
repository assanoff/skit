package rabbitmq_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/matryer/is"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"

	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/broker/rabbitmq"
	"github.com/assanoff/skit/internal/brokertest"
)

// TestPublishConsume sends JSON and binary data through a real RabbitMQ: the
// consumer gets every message it binds with its data, type and topic, and
// records it as a span and in the metrics. The routing key is the type, or
// what WithRoutingKey derives; a message no binding matches is dropped.
func TestPublishConsume(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires docker")
	}

	is := is.New(t)
	ctx := t.Context()
	_, cfg := brokertest.RabbitMQ(ctx, t)
	conn, err := rabbitmq.Dial(cfg)
	is.NoErr(err)
	t.Cleanup(func() { _ = conn.Close() })

	reg := prometheus.NewRegistry()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	got := make(chan broker.Message, 64)
	cons, err := rabbitmq.NewConsumer(slog.New(slog.DiscardHandler), conn, rabbitmq.ConsumerConfig{
		Queue:          "widgets.test",
		Exchange:       "widgets",
		RoutingKeys:    []string{"ping", "widget.created", "custom"},
		Metrics:        reg,
		TracerProvider: tp,
	}, func(_ context.Context, m broker.Message) broker.Action {
		got <- m
		return broker.Ack
	})
	is.NoErr(err)
	is.Equal(cons.Name(), "widgets.test") // the queue names the consumer
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
	// One handler at a time: the first message is done when the second comes.
	is.True(processed(rec, created.ID))
	n, err := testutil.GatherAndCount(reg, "skit_broker_consumed_total")
	is.NoErr(err)
	is.Equal(n, 4) // a series per action of the consumer

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

// processed reports whether rec holds the RabbitMQ consumer span of the
// message id.
func processed(rec *tracetest.SpanRecorder, id string) bool {
	for _, span := range rec.Ended() {
		attrs := attribute.NewSet(span.Attributes()...)
		system, _ := attrs.Value(semconv.MessagingSystemKey)
		msg, _ := attrs.Value(semconv.MessagingMessageIDKey)
		if span.Name() == "process widgets" && system.AsString() == "rabbitmq" && msg.AsString() == id {
			return true
		}
	}
	return false
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
