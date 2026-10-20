package rest_test

import (
	"encoding/json"
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/page"
	"github.com/assanoff/skit/rest"
)

func TestNewResultAndEncode(t *testing.T) {
	is := is.New(t)

	pg := page.New(2, 25)
	r := rest.NewResult([]string{"a", "b"}, 57, pg)

	is.Equal(r.ErrorCode, "ok")                // ok code
	is.Equal(len(r.Data.Items), 2)             // items carried
	is.Equal(r.Data.Pagination.TotalItems, 57) // total echoed
	is.Equal(r.Data.Pagination.CurrentPage, 2) // page echoed
	is.Equal(r.Data.Pagination.Limit, 25)      // rows echoed
	is.Equal(r.Data.Pagination.TotalPages, 3)  // ceil(57/25)

	data, ct, err := r.Encode()
	is.NoErr(err)                    // encodes
	is.Equal(ct, "application/json") // json content-type

	var got map[string]any
	is.NoErr(json.Unmarshal(data, &got)) // round-trips
	is.Equal(got["error_code"], "ok")    // top-level error_code

	d, ok := got["data"].(map[string]any)
	is.True(ok) // data object present
	_, hasItems := d["items"]
	is.True(hasItems) // items under data

	pag, ok := d["pagination"].(map[string]any)
	is.True(ok) // pagination under data
	for _, k := range []string{"total_pages", "current_page", "limit", "total_items"} {
		_, present := pag[k]
		is.True(present) // snake_case pagination key present
	}
}

// TestNewPagination covers the derived total_pages across the edges, and the
// empty case where current_page/limit collapse to zero.
func TestNewPagination(t *testing.T) {
	tests := []struct {
		name              string
		page, rows, total int
		wantPages         int
		wantCurrent       int
		wantLimit         int
	}{
		{"first page", 1, 10, 35, 4, 1, 10},
		{"middle page", 2, 10, 35, 4, 2, 10},
		{"last page", 4, 10, 35, 4, 4, 10},
		{"exact multiple last", 3, 10, 30, 3, 3, 10},
		{"single page", 1, 10, 7, 1, 1, 10},
		{"empty", 1, 10, 0, 0, 0, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			is := is.New(t)

			p := rest.NewPagination(tc.total, page.New(tc.page, tc.rows))
			is.Equal(p.TotalPages, tc.wantPages)    // total pages
			is.Equal(p.CurrentPage, tc.wantCurrent) // current page
			is.Equal(p.Limit, tc.wantLimit)         // limit
			is.Equal(p.TotalItems, tc.total)        // total echoed
		})
	}
}

func TestNewResultItemAndEncode(t *testing.T) {
	is := is.New(t)

	type dto struct {
		ID string `json:"id"`
	}
	r := rest.NewResultItem(dto{ID: "abc"})
	is.Equal(r.ErrorCode, "ok") // ok code
	is.Equal(r.Data.ID, "abc")  // object carried

	data, ct, err := r.Encode()
	is.NoErr(err)
	is.Equal(ct, "application/json")

	var got map[string]any
	is.NoErr(json.Unmarshal(data, &got))
	is.Equal(got["error_code"], "ok")
	d, ok := got["data"].(map[string]any)
	is.True(ok)              // data object present
	is.Equal(d["id"], "abc") // object fields under data
}

func TestNewCursorResultAndEncode(t *testing.T) {
	is := is.New(t)

	r := rest.NewCursorResult([]string{"a"}, "next-token", "")
	is.Equal(r.ErrorCode, "ok")
	is.Equal(len(r.Data.Items), 1)
	is.Equal(r.Data.Next, "next-token")

	data, _, err := r.Encode()
	is.NoErr(err)

	var got map[string]any
	is.NoErr(json.Unmarshal(data, &got))
	is.Equal(got["error_code"], "ok")
	d := got["data"].(map[string]any)
	is.Equal(d["next"], "next-token")
	_, hasPrev := d["prev"]
	is.True(!hasPrev) // prev omitted when empty
}
