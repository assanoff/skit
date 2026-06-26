package query

import (
	"encoding/json"

	"github.com/assanoff/skit/page"
)

// Result is the response envelope for a paginated list query. It implements the
// rest.ResponseEncoder interface (Encode), so a handler can return it directly. T is the
// item DTO type.
//
// TotalPages is the number of pages the total spans at this page size (zero when
// there are no rows). Prev/Next are the adjacent page numbers, each zero (and
// omitted from JSON) when there is no such page — so a client can render
// previous/next links without recomputing the page math.
type Result[T any] struct {
	Items       []T `json:"items"`
	Total       int `json:"total"`
	Page        int `json:"page"`
	RowsPerPage int `json:"rowsPerPage"`
	TotalPages  int `json:"totalPages"`
	Prev        int `json:"prev,omitempty"`
	Next        int `json:"next,omitempty"`
}

// NewResult builds a Result from the page's items, the total row count, and the
// page request that produced them.
func NewResult[T any](items []T, total int, pg page.Page) Result[T] {
	totalPages := numPages(total, pg.RowsPerPage())
	return Result[T]{
		Items:       items,
		Total:       total,
		Page:        pg.Number(),
		RowsPerPage: pg.RowsPerPage(),
		TotalPages:  totalPages,
		Prev:        prevPage(pg.Number()),
		Next:        nextPage(pg.Number(), totalPages),
	}
}

// numPages returns the number of pages needed to hold total rows at the given
// page size, rounding up, or zero when there are no rows. rowsPerPage is always
// positive (page.Page guarantees it via Parse/New).
func numPages(total, rowsPerPage int) int {
	if total <= 0 {
		return 0
	}
	return (total + rowsPerPage - 1) / rowsPerPage
}

// prevPage returns the page before number, or zero when number is the first
// page.
func prevPage(number int) int {
	if number <= 1 {
		return 0
	}
	return number - 1
}

// nextPage returns the page after number, or zero when number is the last page
// (or beyond it).
func nextPage(number, totalPages int) int {
	if number >= totalPages {
		return 0
	}
	return number + 1
}

// Encode implements the rest.ResponseEncoder interface (data, contentType, error).
func (r Result[T]) Encode() ([]byte, string, error) {
	data, err := json.Marshal(r)
	return data, "application/json", err
}

// CursorResult is the cursor-paginated counterpart of Result: the page's items
// plus opaque cursors (page.EncodeCursor) for the next and previous pages, empty
// when there is no such page. It implements rest.ResponseEncoder. Use it with
// page.Cursor when offset paging is too costly or unstable under inserts.
type CursorResult[T any] struct {
	Items []T    `json:"items"`
	Next  string `json:"next,omitempty"`
	Prev  string `json:"prev,omitempty"`
}

// NewCursorResult builds a cursor result. next/prev are opaque tokens from
// page.EncodeCursor (pass "" when there is no next/previous page).
func NewCursorResult[T any](items []T, next, prev string) CursorResult[T] {
	return CursorResult[T]{Items: items, Next: next, Prev: prev}
}

// Encode implements the rest.ResponseEncoder interface.
func (r CursorResult[T]) Encode() ([]byte, string, error) {
	data, err := json.Marshal(r)
	return data, "application/json", err
}
