package jsonapi_test

import (
	"fmt"
	"strings"

	"github.com/assanoff/skit/page"
	"github.com/assanoff/skit/rest/jsonapi"
)

// widgetResource is the DTO, tagged for github.com/hashicorp/jsonapi.
type widgetResource struct {
	ID   string `jsonapi:"primary,widgets"`
	Name string `jsonapi:"attr,name"`
}

// Payload encodes one resource, or a collection, as a JSON:API document.
func ExamplePayload() {
	resp := jsonapi.Payload(&widgetResource{ID: "42", Name: "gadget"})

	body, contentType, _ := resp.Encode()
	fmt.Println(contentType)
	fmt.Println(strings.TrimSpace(string(body)))
	// Output:
	// application/vnd.api+json
	// {"data":{"type":"widgets","id":"42","attributes":{"name":"gadget"}}}
}

// NewResult puts a page of a list into a document and its pagination into
// meta.
func ExampleNewResult() {
	items := []*widgetResource{{ID: "3", Name: "gear"}, {ID: "4", Name: "bolt"}}
	resp := jsonapi.NewResult(items, 5, page.MustParse("2", "2"))

	body, _, _ := resp.Encode()
	fmt.Println(string(body))
	// Output:
	// {"data":[{"type":"widgets","id":"3","attributes":{"name":"gear"}},{"type":"widgets","id":"4","attributes":{"name":"bolt"}}],"meta":{"pagination":{"page":2,"size":2,"total_pages":3,"total_results":5}}}
}
