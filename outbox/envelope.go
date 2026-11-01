package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
	"uuid"

	"github.com/assanoff/skit/otel"
	"github.com/assanoff/skit/queue"
)

// Codec encodes the data of an event. ContentType names the encoding: the
// message goes out with it as its content type, and a consumer decodes the
// data by it.
type Codec interface {
	ContentType() string
	Marshal(v any) ([]byte, error)
}

// jsonCodec is the default Codec, encoding/json.
type jsonCodec struct{}

func (jsonCodec) ContentType() string { return "application/json" }

func (jsonCodec) Marshal(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("json codec: %w", err)
	}
	return b, nil
}

// envelope is the Payload of an event task: the broker message to come, but
// its type — the kind of the task — and its topic — the one of the Handler.
// Data is base64 in the JSON, whatever the codec.
type envelope struct {
	ID          string            `json:"id"`
	Time        time.Time         `json:"time"`
	ContentType string            `json:"content_type"`
	Headers     map[string]string `json:"headers,omitempty"`
	Data        []byte            `json:"data"`
}

// EnvelopeOption customizes an Envelope.
type EnvelopeOption func(*envelopeEncoder)

// WithCodec sets how the data of an event is encoded (default JSON). A service
// usually writes its events in one format, such as protobuf.
func WithCodec(c Codec) EnvelopeOption {
	return func(e *envelopeEncoder) {
		if c != nil {
			e.codec = c
		}
	}
}

// Envelope returns the queue.Encoder of events: it writes an event into the
// Payload of its task with a fresh id, the time of Schedule, the data encoded
// by the codec and its content type, and the W3C trace context of the ctx of
// Schedule — the context the event is created in. Register each event type
// with it:
//
//	reg.Register[widget.Created]("events", "widget.created",
//	    queue.WithEncoder(outbox.Envelope()))
func Envelope(opts ...EnvelopeOption) queue.Encoder {
	e := envelopeEncoder{codec: jsonCodec{}}
	for _, opt := range opts {
		opt(&e)
	}
	return e
}

// envelopeEncoder is the queue.Encoder of Envelope.
type envelopeEncoder struct{ codec Codec }

func (e envelopeEncoder) Encode(ctx context.Context, v any) ([]byte, error) {
	data, err := e.codec.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("outbox envelope: %w", err)
	}
	b, err := json.Marshal(envelope{
		ID:          uuid.New().String(),
		Time:        time.Now().UTC(),
		ContentType: e.codec.ContentType(),
		Headers:     otel.Carrier(ctx),
		Data:        data,
	})
	if err != nil {
		return nil, fmt.Errorf("outbox envelope: %w", err)
	}
	return b, nil
}
