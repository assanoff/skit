package consume_test

import (
	"context"
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/broker/internal/consume"
)

// delivery returns a CloudEvents delivery of a message of type typ.
func delivery(t *testing.T, typ string) consume.Delivery {
	t.Helper()
	m := broker.Message{ID: "1", Type: typ, Source: "test", Data: []byte(`{}`)}
	body, headers, err := broker.CloudEvents{}.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return consume.Delivery{Body: body, Headers: headers}
}

// A delivery the envelope rejects is discarded without running the handler.
func TestProcessMalformedDiscards(t *testing.T) {
	is := is.New(t)
	called := false
	d := consume.Delivery{Body: []byte("{not-cloudevents")}
	got := consume.Process(context.Background(), nil, "c", broker.CloudEvents{}, d,
		func(context.Context, broker.Message) broker.Action {
			called = true
			return broker.Requeue
		})
	is.Equal(got, broker.Discard)
	is.True(!called)
}

// The message carries the topic and every header of the delivery, and the
// verdict is the one of the handler.
func TestProcessDecodesDelivery(t *testing.T) {
	is := is.New(t)
	d := delivery(t, "widget.created")
	d.Headers["traceparent"] = "00-abc"
	d.Topic = "widgets"
	var seen broker.Message
	got := consume.Process(context.Background(), nil, "c", broker.CloudEvents{}, d,
		func(_ context.Context, m broker.Message) broker.Action {
			seen = m
			return broker.Discard
		})
	is.Equal(got, broker.Discard)
	is.Equal(seen.Type, "widget.created")
	is.Equal(seen.Topic, "widgets")
	is.Equal(seen.Headers, d.Headers) // content-type and traceparent
}

func TestGuardPanicRequeues(t *testing.T) {
	is := is.New(t)
	got := consume.Guard(context.Background(), nil, "c", broker.Message{ID: "1"},
		func(context.Context, broker.Message) broker.Action { panic("boom") })
	is.Equal(got, broker.Requeue)
}
