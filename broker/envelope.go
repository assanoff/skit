package broker

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"strings"
	"time"
)

// Envelope packs a Message into the body and headers sent over the wire and
// unpacks a delivery back; routing stays with the adapter. The adapters take
// one through their WithEnvelope option and default to CloudEvents.
//
// On publish the adapter sends the headers of Marshal over Message.Headers,
// the headers of the envelope winning; the wire content type goes in the
// header "content-type", which RabbitMQ carries in its content_type property.
// On delivery the adapter passes every header of the delivery, content-type
// included, to Unmarshal and sets them as Message.Headers itself, so an
// envelope only reads its attributes from them. Message.Topic comes from the
// delivery too.
//
// An error of Marshal fails the publish as it is: wrap it in Permanent when
// the message itself cannot be packed. A delivery Unmarshal rejects is
// discarded with a warning.
type Envelope interface {
	Marshal(m Message) (body []byte, headers map[string]string, err error)
	Unmarshal(body []byte, headers map[string]string) (Message, error)
}

const (
	// specVersion is the CloudEvents spec version stamped on every envelope.
	specVersion = "1.0"
	// contentTypeCloudEvents is the wire content type of the structured-mode
	// JSON envelope.
	contentTypeCloudEvents = "application/cloudevents+json"
)

// CloudEvents is the CloudEvents v1.0 structured-mode JSON envelope, the
// default of every adapter: context attributes (id/type/source/...) live at
// the top level of the body, and the business payload goes under "data" when
// ContentType is JSON (empty, application/json, */*+json) and base64 under
// "data_base64" otherwise. Topic and Headers are not part of the body.
type CloudEvents struct{}

// cloudEvent is the CloudEvents v1.0 structured-mode JSON envelope.
// See https://github.com/cloudevents/spec/blob/v1.0/spec.md#context-attributes
type cloudEvent struct {
	SpecVersion     string          `json:"specversion"`
	ID              string          `json:"id"`
	Type            string          `json:"type"`
	Source          string          `json:"source"`
	Time            time.Time       `json:"time"`
	DataContentType string          `json:"datacontenttype,omitempty"`
	Data            json.RawMessage `json:"data,omitempty"`
	DataBase64      []byte          `json:"data_base64,omitempty"`
}

// Marshal serializes m as a structured-mode JSON envelope, with the header
// content-type application/cloudevents+json. Its errors are Permanent: they
// concern the message itself.
func (CloudEvents) Marshal(m Message) ([]byte, map[string]string, error) {
	if m.Type == "" {
		return nil, nil, Permanent(errors.New("broker: marshal cloudevent: type is required"))
	}
	ce := cloudEvent{
		SpecVersion:     specVersion,
		ID:              m.ID,
		Type:            m.Type,
		Source:          m.Source,
		Time:            m.Time,
		DataContentType: m.ContentType,
	}
	switch {
	case !jsonData(m.ContentType):
		ce.DataBase64 = m.Data
	case len(m.Data) == 0:
		ce.Data = json.RawMessage("null")
	default:
		ce.Data = m.Data
	}
	body, err := json.Marshal(ce)
	if err != nil {
		return nil, nil, Permanent(fmt.Errorf("broker: marshal cloudevent: %w", err))
	}
	return body, map[string]string{"content-type": contentTypeCloudEvents}, nil
}

// jsonData reports whether data of content type ct is JSON, which the envelope
// carries as is: CloudEvents reads an absent type as application/json, and any
// */json or */*+json type is JSON.
func jsonData(ct string) bool {
	if ct == "" {
		return true
	}
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	return strings.HasSuffix(mt, "/json") || strings.HasSuffix(mt, "+json")
}

// Unmarshal parses a structured-mode JSON envelope back into a Message, taking
// Data from "data" or, decoded, from "data_base64". It reads no headers; Topic
// and Headers are left zero for the adapter to fill from the delivery.
func (CloudEvents) Unmarshal(body []byte, _ map[string]string) (Message, error) {
	var ce cloudEvent
	if err := json.Unmarshal(body, &ce); err != nil {
		return Message{}, fmt.Errorf("broker: unmarshal cloudevent: %w", err)
	}
	if ce.SpecVersion == "" || ce.ID == "" || ce.Type == "" {
		return Message{}, errors.New("broker: unmarshal cloudevent: missing required envelope fields")
	}
	data := []byte(ce.Data)
	if ce.DataBase64 != nil {
		data = ce.DataBase64
	}
	return Message{
		ID:          ce.ID,
		Type:        ce.Type,
		Source:      ce.Source,
		Time:        ce.Time,
		ContentType: ce.DataContentType,
		Data:        data,
	}, nil
}
