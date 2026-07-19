package query

import (
	"encoding/json"
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/page"
)

// widgetJSONAPI is a JSON:API-tagged DTO for the ResultJSONAPI tests.
type widgetJSONAPI struct {
	ID   string `jsonapi:"primary,widgets"`
	Name string `jsonapi:"attr,name"`
}

func TestNewResultJSONAPIAndEncode(t *testing.T) {
	is := is.New(t)

	items := []*widgetJSONAPI{{ID: "1", Name: "a"}, {ID: "2", Name: "b"}}
	r := NewResultJSONAPI(items, 57, page.New(2, 25))

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

// TestNewResultJSONAPIEmpty checks the empty result collapses page/size to zero
// and still emits a well-formed JSON:API document.
func TestNewResultJSONAPIEmpty(t *testing.T) {
	is := is.New(t)

	r := NewResultJSONAPI([]*widgetJSONAPI{}, 0, page.New(1, 10))
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
