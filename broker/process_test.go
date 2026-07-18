package broker_test

import (
	"context"
	"testing"

	"github.com/assanoff/skit/broker"
)

func ceBody(t *testing.T, typ string) []byte {
	t.Helper()
	b, err := broker.MarshalCloudEvent(broker.Message{ID: "1", Type: typ, Source: "test", Data: []byte(`{}`)})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestProcessMalformedDrops(t *testing.T) {
	called := false
	got := broker.Process(context.Background(), nil, "c", []byte("{not-cloudevents"), nil,
		func(context.Context, broker.Message) broker.Action { called = true; return broker.Requeue })
	if got != broker.Ack {
		t.Errorf("malformed action = %v, want Ack (drop)", got)
	}
	if called {
		t.Error("handler must not run for a malformed envelope")
	}
}

func TestProcessRoutesAndReturnsHandlerAction(t *testing.T) {
	var seen broker.Message
	got := broker.Process(context.Background(), nil, "c", ceBody(t, "widget.created"),
		func(m *broker.Message) { m.Topic = "widgets"; m.Key = "k" },
		func(_ context.Context, m broker.Message) broker.Action { seen = m; return broker.Discard })
	if got != broker.Discard {
		t.Errorf("action = %v, want Discard (handler's verdict)", got)
	}
	if seen.Topic != "widgets" || seen.Key != "k" {
		t.Errorf("route not applied: topic=%q key=%q", seen.Topic, seen.Key)
	}
	if seen.Type != "widget.created" {
		t.Errorf("decoded type = %q", seen.Type)
	}
}

func TestGuardPanicRequeues(t *testing.T) {
	got := broker.Guard(context.Background(), nil, "c", broker.Message{ID: "1"},
		func(context.Context, broker.Message) broker.Action { panic("boom") })
	if got != broker.Requeue {
		t.Errorf("panic action = %v, want Requeue", got)
	}
}
