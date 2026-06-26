package to

import (
	"encoding/json"
	"net/http"
	"testing"
)

type widgetDTO struct {
	ID   string `jsonapi:"primary,widgets"`
	Name string `jsonapi:"attr,name"`
}

func TestJSON(t *testing.T) {
	enc := JSON(map[string]int{"n": 1})
	data, ct, err := enc.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}
	if string(data) != `{"n":1}` {
		t.Fatalf("body = %s", data)
	}
}

func TestJSONStatus(t *testing.T) {
	enc := JSONStatus(struct{}{}, http.StatusCreated)
	hs, ok := enc.(interface{ HTTPStatus() int })
	if !ok || hs.HTTPStatus() != http.StatusCreated {
		t.Fatalf("expected status 201 encoder, got %T", enc)
	}
}

func TestJSONAPISingle(t *testing.T) {
	data, ct, err := JSONAPI(&widgetDTO{ID: "1", Name: "gadget"}).Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if ct != "application/vnd.api+json" {
		t.Fatalf("content-type = %q", ct)
	}

	var doc struct {
		Data struct {
			Type       string         `json:"type"`
			ID         string         `json:"id"`
			Attributes map[string]any `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if doc.Data.Type != "widgets" || doc.Data.ID != "1" || doc.Data.Attributes["name"] != "gadget" {
		t.Fatalf("jsonapi doc = %+v", doc.Data)
	}
}

func TestJSONAPICollection(t *testing.T) {
	data, _, err := JSONAPI([]*widgetDTO{{ID: "1", Name: "a"}, {ID: "2", Name: "b"}}).Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var doc struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(doc.Data) != 2 {
		t.Fatalf("data len = %d, want 2", len(doc.Data))
	}
}
