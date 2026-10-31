package broker

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/matryer/is"
)

func TestCloudEventRoundTrip(t *testing.T) {
	in := Message{
		ID:          "evt-1",
		Type:        "widget.created",
		Source:      "skit-example",
		Time:        time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC),
		ContentType: "application/json",
		Data:        []byte(`{"id":"42","name":"gadget"}`),
		// Transport fields are not part of the envelope and must not survive a
		// marshal/unmarshal round-trip.
		Topic:   "ex",
		Headers: map[string]string{"traceparent": "abc"},
	}

	body, err := MarshalCloudEvent(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// The envelope is structured-mode CloudEvents JSON.
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("envelope is not JSON: %v", err)
	}
	if raw["specversion"] != SpecVersion {
		t.Errorf("specversion = %v, want %s", raw["specversion"], SpecVersion)
	}
	if _, ok := raw["data"]; !ok {
		t.Error("envelope missing data field")
	}
	if _, ok := raw["topic"]; ok {
		t.Error("transport field topic leaked into the envelope")
	}

	out, err := UnmarshalCloudEvent(body)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.ID != in.ID || out.Type != in.Type || out.Source != in.Source ||
		out.ContentType != in.ContentType {
		t.Errorf("context attributes mismatch: %+v", out)
	}
	if !out.Time.Equal(in.Time) {
		t.Errorf("time mismatch: got %s want %s", out.Time, in.Time)
	}
	if string(out.Data) != string(in.Data) {
		t.Errorf("data mismatch: got %s want %s", out.Data, in.Data)
	}
	// Transport fields come back zero (they're filled from the delivery).
	if out.Topic != "" || out.Headers != nil {
		t.Errorf("transport fields should be zero after unmarshal: %+v", out)
	}
}

func TestMarshalRequiresType(t *testing.T) {
	if _, err := MarshalCloudEvent(Message{Data: []byte(`{}`)}); err == nil {
		t.Error("expected error when type is empty")
	}
}

func TestUnmarshalRejectsMissingFields(t *testing.T) {
	// Valid JSON but missing required envelope fields.
	if _, err := UnmarshalCloudEvent([]byte(`{"data":{}}`)); err == nil {
		t.Error("expected error for envelope missing specversion/id/type")
	}
	if _, err := UnmarshalCloudEvent([]byte(`not json`)); err == nil {
		t.Error("expected error for non-JSON body")
	}
}

func TestMarshalEmptyDataIsNull(t *testing.T) {
	body, err := MarshalCloudEvent(Message{Type: "x"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["data"]) != "null" {
		t.Errorf("empty data should marshal to null, got %s", raw["data"])
	}
}

func TestCloudEventBinaryData(t *testing.T) {
	is := is.New(t)
	data := []byte{0x08, 0x03, 0xff, 0x00}

	body, err := MarshalCloudEvent(Message{
		ID: "1", Type: "x", Source: "s",
		ContentType: "application/protobuf",
		Data:        data,
	})
	is.NoErr(err)
	var raw map[string]json.RawMessage
	is.NoErr(json.Unmarshal(body, &raw))
	is.Equal(string(raw["data_base64"]), `"CAP/AA=="`) // base64, as the spec asks
	_, ok := raw["data"]
	is.True(!ok) // data and data_base64 never come together

	out, err := UnmarshalCloudEvent(body)
	is.NoErr(err)
	is.Equal(out.Data, data)
	is.Equal(out.ContentType, "application/protobuf")
}

func TestCloudEventJSONTypes(t *testing.T) {
	is := is.New(t)
	for _, ct := range []string{"", "application/json", "application/json; charset=utf-8", "application/vnd.x+json"} {
		body, err := MarshalCloudEvent(Message{Type: "x", ContentType: ct, Data: []byte(`{"a":1}`)})
		is.NoErr(err)
		var raw map[string]json.RawMessage
		is.NoErr(json.Unmarshal(body, &raw))
		is.Equal(string(raw["data"]), `{"a":1}`) // JSON travels as is
	}
}

func TestMarshalErrorsArePermanent(t *testing.T) {
	is := is.New(t)

	_, err := MarshalCloudEvent(Message{Data: []byte(`{}`)})
	is.True(IsPermanent(err)) // no type

	_, err = MarshalCloudEvent(Message{Type: "x", ContentType: "application/json", Data: []byte(`{bad`)})
	is.True(err != nil)
	is.True(IsPermanent(err)) // the data is not the JSON its type claims
}
