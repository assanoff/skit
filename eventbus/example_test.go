package eventbus_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/assanoff/skit/eventbus"
)

type userCreated struct {
	ID string `json:"id"`
}

// A consumer registers for (domain, action) and decodes the payload; the
// producer knows only the event, never the consumer.
func Example() {
	bus := eventbus.New(slog.New(slog.DiscardHandler))
	bus.Register("user", "created", func(_ context.Context, d eventbus.Data) error {
		ev, err := d.Decode[userCreated]()
		if err != nil {
			return err
		}
		fmt.Println("send welcome to", ev.ID)
		return nil
	})

	evt := eventbus.MustData("user", "created", userCreated{ID: "u-1"})
	fmt.Println(evt)
	if err := bus.Call(context.Background(), evt); err != nil {
		fmt.Println(err)
	}
	// Output:
	// eventbus.Data{Domain:"user", Action:"created", RawParams:{"id":"u-1"}}
	// send welcome to u-1
}

// Call stops at the first failed handler; Publish runs every handler and joins
// their errors.
func ExampleBus_Publish() {
	bus := eventbus.New(slog.New(slog.DiscardHandler))
	for _, name := range []string{"mailer", "crm", "metrics"} {
		bus.Register("user", "created", func(context.Context, eventbus.Data) error {
			fmt.Println("run", name)
			if name == "mailer" {
				return errors.New("mailer: smtp down")
			}
			return nil
		})
	}
	evt := eventbus.MustData("user", "created", userCreated{ID: "u-1"})
	ctx := context.Background()

	fmt.Println("call:", bus.Call(ctx, evt))
	fmt.Println("publish:", bus.Publish(ctx, evt))
	// Output:
	// run mailer
	// call: eventbus: user/created: mailer: smtp down
	// run mailer
	// run crm
	// run metrics
	// publish: eventbus: user/created: mailer: smtp down
}
