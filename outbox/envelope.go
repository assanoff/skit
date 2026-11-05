package outbox

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"time"
	"uuid"

	"github.com/assanoff/skit/otel"
	"github.com/assanoff/skit/queue"
)

// envelope is the Payload of an event task: the broker message to come, but
// its type — the kind of the task — and its topic — the one of the Handler.
// Data is base64 in the JSON, whatever its content type.
type envelope struct {
	ID          string            `json:"id"`
	Time        time.Time         `json:"time"`
	ContentType string            `json:"content_type"`
	Headers     map[string]string `json:"headers,omitempty"`
	Data        []byte            `json:"data"`
}

// JSON is the queue.Encoder of events with JSON data: the Envelope of
// encoding/json/v2 with map keys sorted, content type application/json.
// Register each event type with it:
//
//	reg.Register[widget.Created]("events", "widget.created",
//	    queue.WithEncoder(outbox.JSON))
func JSON(ctx context.Context, v any) ([]byte, error) {
	return encode(ctx, v, "application/json", marshalJSON)
}

// marshalJSON encodes the data of a JSON event, the same value to the same
// bytes.
func marshalJSON(v any) ([]byte, error) {
	return json.Marshal(v, json.Deterministic(true)) //nolint:wrapcheck // encode wraps it
}

// Envelope returns the queue.Encoder of events with data of another format,
// such as protobuf: marshal encodes the data, and contentType names the
// encoding — the message goes out with it, and a consumer decodes the data by
// it. The encoder writes an event into the Payload of its task with a fresh
// id, the time of Schedule, the encoded data and the W3C trace context of the
// ctx of Schedule — the context the event is created in.
func Envelope(contentType string, marshal func(v any) ([]byte, error)) queue.Encoder {
	return func(ctx context.Context, v any) ([]byte, error) {
		return encode(ctx, v, contentType, marshal)
	}
}

// encode writes v into an envelope, its data encoded by marshal.
func encode(
	ctx context.Context, v any, contentType string, marshal func(v any) ([]byte, error),
) ([]byte, error) {
	data, err := marshal(v)
	if err != nil {
		return nil, fmt.Errorf("outbox envelope: %w", err)
	}
	b, err := json.Marshal(envelope{
		ID:          uuid.New().String(),
		Time:        time.Now().UTC(),
		ContentType: contentType,
		Headers:     otel.Carrier(ctx),
		Data:        data,
	}, json.Deterministic(true))
	if err != nil {
		return nil, fmt.Errorf("outbox envelope: %w", err)
	}
	return b, nil
}
