package kafka

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/matryer/is"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	kgo "github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"

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
	is := is.New(t)
	cfg := Config{Brokers: []string{"b"}}
	for _, tc := range []struct {
		cfg  Config
		ccfg ConsumerConfig
	}{
		{Config{}, ConsumerConfig{Topic: "t", Group: "g"}},
		{cfg, ConsumerConfig{Group: "g"}},
		{cfg, ConsumerConfig{Topic: "t"}},
	} {
		_, err := NewConsumer(testLog(), tc.cfg, tc.ccfg, noopHandler)
		is.True(err != nil) // brokers, topic and group are required
	}
}

func TestNewConsumerDefaults(t *testing.T) {
	is := is.New(t)
	c, err := NewConsumer(testLog(), Config{Brokers: []string{"b"}},
		ConsumerConfig{Topic: "orders", Group: "billing"}, noopHandler)
	is.NoErr(err)
	is.Equal(c.Name(), "billing") // the group names the consumer
	is.Equal(c.cfg.Concurrency, 1)
}

const consumedWant = `
# HELP skit_broker_consumed_total Messages handled, by consumer and action: ack, requeue, discard or panic.
# TYPE skit_broker_consumed_total counter
skit_broker_consumed_total{action="ack",consumer="billing"} 1
skit_broker_consumed_total{action="discard",consumer="billing"} 0
skit_broker_consumed_total{action="panic",consumer="billing"} 0
skit_broker_consumed_total{action="requeue",consumer="billing"} 0
`

// The consumer instruments its handler: a record is a span "process <topic>"
// of messaging.system kafka and a record in skit_broker_* under the name of
// the consumer.
func TestConsumerInstrumentsHandler(t *testing.T) {
	is := is.New(t)
	reg := prometheus.NewRegistry()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	c, err := NewConsumer(testLog(), Config{Brokers: []string{"b"}}, ConsumerConfig{
		Topic: "orders", Group: "billing", Metrics: reg, TracerProvider: tp,
	}, noopHandler)
	is.NoErr(err)

	is.Equal(c.dispatch(t.Context(), broker.Message{ID: "1", Topic: "orders"}), broker.Ack)

	is.NoErr(testutil.GatherAndCompare(reg, strings.NewReader(consumedWant), "skit_broker_consumed_total"))
	spans := rec.Ended()
	is.Equal(len(spans), 1)
	is.Equal(spans[0].Name(), "process orders")
	attrs := attribute.NewSet(spans[0].Attributes()...)
	system, _ := attrs.Value(semconv.MessagingSystemKey)
	is.Equal(system.AsString(), "kafka")
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
	c, err := NewConsumer(log, Config{Brokers: []string{"127.0.0.1:1"}},
		ConsumerConfig{Topic: "t", Group: "g"}, noopHandler)
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

func TestPublishTooLargeIsPermanent(t *testing.T) {
	is := is.New(t)
	p, err := NewPublisher(Config{Brokers: []string{"localhost:1"}}, PublisherConfig{Source: "test"})
	is.NoErr(err)
	t.Cleanup(func() { _ = p.Close() })

	// Over BatchBytes of the writer (1 MiB): rejected before any dial.
	err = p.Publish(t.Context(), broker.Message{
		Type: "x", Topic: "t",
		ContentType: "application/octet-stream",
		Data:        make([]byte, 2<<20),
	})
	is.True(broker.IsPermanent(err))
}

func TestPartitionKeyError(t *testing.T) {
	is := is.New(t)
	keyErr := errors.New("no id")
	for _, tc := range []struct {
		err       error
		permanent bool
	}{
		{keyErr, false},                  // the function may be wrong: retried
		{broker.Permanent(keyErr), true}, // the message has no key: failed
	} {
		key := func(broker.Message) (string, error) {
			return "", tc.err
		}
		p, err := NewPublisher(Config{Brokers: []string{"localhost:1"}},
			PublisherConfig{Source: "test"}, WithPartitionKey(key))
		is.NoErr(err)
		t.Cleanup(func() { _ = p.Close() })

		// The key comes before any dial.
		err = p.Publish(t.Context(), broker.Message{Type: "x", Topic: "t"})
		is.True(errors.Is(err, keyErr))
		is.Equal(broker.IsPermanent(err), tc.permanent)
	}
}

func TestTooLarge(t *testing.T) {
	is := is.New(t)
	is.True(tooLarge(kgo.WriteErrors{kgo.MessageSizeTooLarge})) // from the brokers
	is.True(!tooLarge(kgo.WriteErrors{kgo.LeaderNotAvailable}))
	is.True(!tooLarge(context.DeadlineExceeded))
}
