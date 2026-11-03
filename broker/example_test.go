package broker_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/assanoff/skit/broker"
)

// The CloudEvents envelope packs a Message into the body sent over the wire,
// with its attributes at the top level and JSON data under "data". A consumer
// unpacks the body back into a Message.
func Example() {
	var env broker.Envelope = broker.CloudEvents{}
	m := broker.Message{
		ID:          "e-1",
		Type:        "widget.created",
		Source:      "shop",
		Time:        time.Date(2026, 11, 1, 9, 30, 0, 0, time.UTC),
		ContentType: "application/json",
		Data:        []byte(`{"id":"w-1"}`),
		Topic:       "widgets",
	}

	body, headers, err := env.Marshal(m)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(body))
	fmt.Println(headers)

	got, err := env.Unmarshal(body, headers)
	fmt.Println(got.ID, got.Type, string(got.Data), err)
	// Output:
	// {"specversion":"1.0","id":"e-1","type":"widget.created","source":"shop","time":"2026-11-01T09:30:00Z","datacontenttype":"application/json","data":{"id":"w-1"}}
	// map[content-type:application/cloudevents+json]
	// e-1 widget.created {"id":"w-1"} <nil>
}

// A Handler returns the verdict on a delivery: Ack drops it, Requeue has the
// broker deliver it again, Discard drops it or sends it to a dead-letter
// exchange.
func ExampleHandler() {
	errDown := errors.New("billing is down")
	charge := func(id string) error {
		if id == "w-2" {
			return errDown
		}
		return nil
	}
	h := func(_ context.Context, m broker.Message) broker.Action {
		var v struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(m.Data, &v); err != nil {
			return broker.Discard // it never decodes
		}
		if err := charge(v.ID); err != nil {
			return broker.Requeue // a later delivery may succeed
		}
		return broker.Ack
	}

	verdict := map[broker.Action]string{
		broker.Ack: "ack", broker.Requeue: "requeue", broker.Discard: "discard",
	}
	ctx := context.Background()
	for _, data := range []string{`{"id":"w-1"}`, `{"id":"w-2"}`, `{"id":`} {
		fmt.Println(data, verdict[h(ctx, broker.Message{Data: []byte(data)})])
	}
	// Output:
	// {"id":"w-1"} ack
	// {"id":"w-2"} requeue
	// {"id": discard
}

// Permanent marks an error no retry fixes: the message itself cannot be
// delivered. The mark survives wrapping.
func ExamplePermanent() {
	errTooLarge := errors.New("message over 1 MiB")
	err := fmt.Errorf("publish widget.created: %w", broker.Permanent(errTooLarge))

	fmt.Println(err)
	fmt.Println(broker.IsPermanent(err), errors.Is(err, errTooLarge))
	fmt.Println(broker.IsPermanent(errors.New("connection refused")))
	// Output:
	// publish widget.created: message over 1 MiB
	// true true
	// false
}
