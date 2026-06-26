package query

import (
	"encoding/json"
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/page"
)

func TestNewResultAndEncode(t *testing.T) {
	is := is.New(t)

	pg := page.New(2, 25)
	r := NewResult([]string{"a", "b"}, 57, pg)

	is.Equal(r.Total, 57)       // total echoed
	is.Equal(r.Page, 2)         // page echoed
	is.Equal(r.RowsPerPage, 25) // rows echoed
	is.Equal(len(r.Items), 2)   // items carried
	is.Equal(r.TotalPages, 3)   // ceil(57/25)
	is.Equal(r.Prev, 1)         // page 2 -> prev 1
	is.Equal(r.Next, 3)         // page 2 -> next 3 (last)

	data, ct, err := r.Encode()
	is.NoErr(err)                    // encodes
	is.Equal(ct, "application/json") // json content-type

	var got map[string]any
	is.NoErr(json.Unmarshal(data, &got)) // round-trips
	for _, k := range []string{"items", "total", "page", "rowsPerPage", "totalPages", "prev", "next"} {
		_, ok := got[k]
		is.True(ok) // present in encoded JSON
	}
}

// TestResultPaginationMath covers the derived TotalPages/Prev/Next across the
// edges: first page, last page, exact multiple, and an empty result.
func TestResultPaginationMath(t *testing.T) {
	tests := []struct {
		name                string
		page, rows, total   int
		wantPages, wantPrev int
		wantNext            int
	}{
		{"first page", 1, 10, 35, 4, 0, 2},
		{"middle page", 2, 10, 35, 4, 1, 3},
		{"last page", 4, 10, 35, 4, 3, 0},
		{"exact multiple last", 3, 10, 30, 3, 2, 0},
		{"single page", 1, 10, 7, 1, 0, 0},
		{"empty", 1, 10, 0, 0, 0, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			is := is.New(t)

			r := NewResult([]int(nil), tc.total, page.New(tc.page, tc.rows))
			is.Equal(r.TotalPages, tc.wantPages) // total pages
			is.Equal(r.Prev, tc.wantPrev)        // prev page (0 = none)
			is.Equal(r.Next, tc.wantNext)        // next page (0 = none)
		})
	}
}

// TestResultPrevNextOmitted verifies prev/next are dropped from the JSON when
// zero (no such page), so the envelope stays clean on the first/last page.
func TestResultPrevNextOmitted(t *testing.T) {
	is := is.New(t)

	r := NewResult([]int{1}, 5, page.New(1, 10)) // single page: no prev, no next
	data, _, err := r.Encode()
	is.NoErr(err)

	var got map[string]any
	is.NoErr(json.Unmarshal(data, &got))
	_, hasPrev := got["prev"]
	_, hasNext := got["next"]
	is.True(!hasPrev) // prev omitted when zero
	is.True(!hasNext) // next omitted when zero
}
