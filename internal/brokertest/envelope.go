package brokertest

import (
	"errors"

	"github.com/assanoff/skit/broker"
)

// Raw is an envelope of the tests other than CloudEvents: the body is the data
// as is, the attributes travel in headers.
type Raw struct{}

// Marshal packs m as its data, with the id, the type and the content type in
// headers.
func (Raw) Marshal(m broker.Message) ([]byte, map[string]string, error) {
	return m.Data, map[string]string{
		"content-type": m.ContentType,
		"x-id":         m.ID,
		"x-type":       m.Type,
	}, nil
}

// Unmarshal unpacks a message Marshal packed; a delivery without the type
// header is not one.
func (Raw) Unmarshal(body []byte, headers map[string]string) (broker.Message, error) {
	if headers["x-type"] == "" {
		return broker.Message{}, errors.New("brokertest: not a raw message")
	}
	return broker.Message{
		ID:          headers["x-id"],
		Type:        headers["x-type"],
		ContentType: headers["content-type"],
		Data:        body,
	}, nil
}
