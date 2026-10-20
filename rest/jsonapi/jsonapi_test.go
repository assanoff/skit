package jsonapi_test

import (
	"encoding/json"
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/page"
	"github.com/assanoff/skit/rest/jsonapi"
)

// widget is a JSON:API-tagged DTO for the tests.
type widget struct {
	ID   string `jsonapi:"primary,widgets"`
	Name string `jsonapi:"attr,name"`
}

func TestPayloadSingle(t *testing.T) {
	is := is.New(t)

	data, ct, err := jsonapi.Payload(&widget{ID: "1", Name: "gadget"}).Encode()
	is.NoErr(err)
	is.Equal(ct, "application/vnd.api+json") // JSON:API media type

	var doc struct {
		Data struct {
			Type       string         `json:"type"`
			ID         string         `json:"id"`
			Attributes map[string]any `json:"attributes"`
		} `json:"data"`
	}
	is.NoErr(json.Unmarshal(data, &doc))
	is.Equal(doc.Data.Type, "widgets")              // type from the primary tag
	is.Equal(doc.Data.ID, "1")                      // resource id
	is.Equal(doc.Data.Attributes["name"], "gadget") // attribute
}

func TestPayloadCollection(t *testing.T) {
	is := is.New(t)

	items := []*widget{{ID: "1", Name: "a"}, {ID: "2", Name: "b"}}
	data, _, err := jsonapi.Payload(items).Encode()
	is.NoErr(err)

	var doc struct {
		Data []map[string]any `json:"data"`
	}
	is.NoErr(json.Unmarshal(data, &doc))
	is.Equal(len(doc.Data), 2) // a resource per item
}

func TestPayloadNotTagged(t *testing.T) {
	is := is.New(t)

	_, _, err := jsonapi.Payload(struct{}{}).Encode()
	is.True(err != nil) // not a pointer to a tagged struct
}

func TestNewResultAndEncode(t *testing.T) {
	is := is.New(t)

	items := []*widget{{ID: "1", Name: "a"}, {ID: "2", Name: "b"}}
	r := jsonapi.NewResult(items, 57, page.New(2, 25))

	data, ct, err := r.Encode()
	is.NoErr(err)
	is.Equal(ct, "application/vnd.api+json") // JSON:API media type

	var got map[string]any
	is.NoErr(json.Unmarshal(data, &got))

	// data is a JSON:API resource array
	arr, ok := got["data"].([]any)
	is.True(ok)
	is.Equal(len(arr), 2)
	first := arr[0].(map[string]any)
	is.Equal(first["type"], "widgets") // resource type from the primary tag
	is.Equal(first["id"], "1")         // resource id

	// pagination lives under meta.pagination
	meta, ok := got["meta"].(map[string]any)
	is.True(ok)
	pag, ok := meta["pagination"].(map[string]any)
	is.True(ok)
	is.Equal(pag["page"].(float64), float64(2))           // page number
	is.Equal(pag["size"].(float64), float64(25))          // page size
	is.Equal(pag["total_pages"].(float64), float64(3))    // ceil(57/25)
	is.Equal(pag["total_results"].(float64), float64(57)) // total
}

// TestNewResultEmpty checks the empty result collapses page/size to zero and
// still emits a well-formed JSON:API document.
func TestNewResultEmpty(t *testing.T) {
	is := is.New(t)

	r := jsonapi.NewResult([]*widget{}, 0, page.New(1, 10))
	data, _, err := r.Encode()
	is.NoErr(err)

	var got map[string]any
	is.NoErr(json.Unmarshal(data, &got))
	pag := got["meta"].(map[string]any)["pagination"].(map[string]any)
	is.Equal(pag["total_results"].(float64), float64(0))
	is.Equal(pag["total_pages"].(float64), float64(0))
	is.Equal(pag["page"].(float64), float64(0))
	is.Equal(pag["size"].(float64), float64(0))
}
