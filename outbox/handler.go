package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/queue"
)

// Handler returns the queue.JobFunc that publishes an event task to topic
// through pub. The message carries the id, time, content type, data and trace
// context of the Envelope of the task, and the kind of the task as its type;
// the broker.Publisher of an adapter sends it under a CLIENT child of the
// context the event was created in.
//
// The error decides the outcome of the task: an envelope that does not decode
// and an error marked broker.Permanent are queue.Permanent — the task goes to
// the dead letters; any other error is queue.Halt — the broker is likely down,
// so the batch stops and the task is retried with backoff.
func Handler(pub broker.Publisher, topic string) queue.JobFunc {
	return func(ctx context.Context, t queue.Task) error {
		var env envelope
		if err := json.Unmarshal(t.Payload, &env); err != nil {
			return queue.Permanent(fmt.Errorf("outbox: decode envelope of %s: %w", t.Kind, err))
		}
		err := pub.Publish(ctx, broker.Message{
			ID:          env.ID,
			Type:        t.Kind,
			Time:        env.Time,
			ContentType: env.ContentType,
			Data:        env.Data,
			Topic:       topic,
			Headers:     env.Headers,
		})
		if broker.IsPermanent(err) {
			return queue.Permanent(err)
		}
		return queue.Halt(err)
	}
}

// Config returns the settings of a queue of events named name: a batch of 100
// events, 5 seconds for a publish and for the batch, a poll every 2 seconds
// when idle. Change a field when the broker needs it.
//
// Do not give the queue a backoff with MaxAttempts (queue.WithBackoff): a
// broker down for longer than the attempts last would send the events of the
// outage to the dead letters. Without it only broker.Permanent and an envelope
// that does not decode do.
func Config(name string) queue.Config {
	return queue.Config{
		Queue:   name,
		Batch:   100,
		Timeout: 5 * time.Second,
		Poll:    2 * time.Second,
	}
}
