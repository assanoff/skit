package auditbus_test

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"

	"github.com/assanoff/skit/auditlog"
	"github.com/assanoff/skit/auditlog/auditbus"
	"github.com/assanoff/skit/auditlog/audittest"
	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/eventbus"
)

// The producer publishes an Event on the bus and does not import auditlog;
// Register, wired once at startup, writes it through the recorder.
func Example() {
	ctx := context.Background()
	core := auditlog.NewCore(nil, audittest.NewStore())
	bus := eventbus.New(nil)
	auditbus.Register(bus, core)

	err := auditbus.Publish(ctx, bus, auditbus.Event{
		ModelType: "widget",
		ModelID:   "w-1",
		CreatedBy: "ann",
		Payload:   jsontext.Value(`{"name":"gear"}`),
	})
	fmt.Println("publish:", err)

	last, err := core.QueryLastByModelID(ctx, "widget", "w-1")
	fmt.Println(last.Version, last.CreatedBy, string(bytes.TrimSpace(last.Payload)), err)
	// Output:
	// publish: <nil>
	// 1 ann {"name":"gear"} <nil>
}

// Record serves a consumer of another bus: the handler decodes the message
// into an Event, and the error of the write decides the fate of the message.
func ExampleRecord() {
	core := auditlog.NewCore(nil, audittest.NewStore())
	handle := func(ctx context.Context, m broker.Message) broker.Action {
		var ev auditbus.Event
		if err := json.Unmarshal(m.Data, &ev); err != nil {
			return broker.Discard // it never decodes
		}
		if err := auditbus.Record(ctx, core, ev); err != nil {
			return broker.Requeue // a later delivery may succeed
		}
		return broker.Ack
	}

	verdict := map[broker.Action]string{
		broker.Ack: "ack", broker.Requeue: "requeue", broker.Discard: "discard",
	}
	ctx := context.Background()
	for _, data := range []string{
		`{"model_type":"widget","model_id":"w-1","payload":{"name":"gear"}}`,
		`{"model_type":`,
	} {
		fmt.Println(verdict[handle(ctx, broker.Message{Data: []byte(data)})])
	}

	last, err := core.QueryLastByModelID(ctx, "widget", "w-1")
	fmt.Println(last.Version, string(bytes.TrimSpace(last.Payload)), err)
	// Output:
	// ack
	// discard
	// 1 {"name":"gear"} <nil>
}
