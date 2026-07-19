package query

import (
	"encoding/json"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/page"
)

const contentTypeJSON = "application/json"

// Pagination is the offset-paging metadata embedded in a list envelope.
type Pagination struct {
	TotalPages  int `json:"total_pages"`
	CurrentPage int `json:"current_page"`
	Limit       int `json:"limit"`
	TotalItems  int `json:"total_items"`
}

// Data is the payload of a paginated list envelope: the page's items and their
// pagination metadata.
type Data[T any] struct {
	Items      []T        `json:"items"`
	Pagination Pagination `json:"pagination"`
}

// Result is the response envelope for a paginated list query: an error_code and a
// data object holding the items and their pagination. It implements the
// rest.ResponseEncoder interface (Encode), so a handler can return it directly. T
// is the item DTO type. The shape mirrors the house convention:
//
//	{"error_code":"ok","data":{"items":[...],"pagination":{"total_pages":..,"current_page":..,"limit":..,"total_items":..}}}
type Result[T any] struct {
	ErrorCode string  `json:"error_code"`
	Data      Data[T] `json:"data"`
}

// NewResult builds a list envelope from the page's items, the total row count, and
// the page request that produced them. When the total spans no pages (empty
// result) the pagination reports zeros for current_page and limit.
func NewResult[T any](items []T, total int, pg page.Page) Result[T] {
	totalPages := numPages(total, pg.RowsPerPage())
	if totalPages == 0 {
		pg = page.Page{}
	}
	return Result[T]{
		ErrorCode: errs.OK.String(),
		Data: Data[T]{
			Items: items,
			Pagination: Pagination{
				TotalPages:  totalPages,
				CurrentPage: pg.Number(),
				Limit:       pg.RowsPerPage(),
				TotalItems:  total,
			},
		},
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

// Encode implements the rest.ResponseEncoder interface (data, contentType, error).
func (r Result[T]) Encode() ([]byte, string, error) {
	data, err := json.Marshal(r)
	return data, contentTypeJSON, err
}

// ResultItem is the response envelope for a single-object query: an error_code and
// the object as data. It implements rest.ResponseEncoder (Encode). The shape is
// {"error_code":"ok","data":{...}}.
type ResultItem[T any] struct {
	ErrorCode string `json:"error_code"`
	Data      T      `json:"data"`
}

// NewResultItem wraps a single object in a data envelope with an ok error_code.
// Return it from get/create/update handlers so every endpoint shares the
// {error_code, data} shape.
func NewResultItem[T any](data T) ResultItem[T] {
	return ResultItem[T]{ErrorCode: errs.OK.String(), Data: data}
}

// Encode implements the rest.ResponseEncoder interface.
func (r ResultItem[T]) Encode() ([]byte, string, error) {
	data, err := json.Marshal(r)
	return data, contentTypeJSON, err
}

// CursorData is the payload of a cursor-paginated list envelope: the page's items
// plus opaque next/prev cursors (empty when there is no such page).
type CursorData[T any] struct {
	Items []T    `json:"items"`
	Next  string `json:"next,omitempty"`
	Prev  string `json:"prev,omitempty"`
}

// CursorResult is the cursor-paginated counterpart of Result: the same
// {error_code, data} envelope, with data carrying the items and opaque cursors
// (page.EncodeCursor) for the next and previous pages. It implements
// rest.ResponseEncoder. Use it with page.Cursor when offset paging is too costly
// or unstable under inserts.
type CursorResult[T any] struct {
	ErrorCode string        `json:"error_code"`
	Data      CursorData[T] `json:"data"`
}

// NewCursorResult builds a cursor envelope. next/prev are opaque tokens from
// page.EncodeCursor (pass "" when there is no next/previous page).
func NewCursorResult[T any](items []T, next, prev string) CursorResult[T] {
	return CursorResult[T]{
		ErrorCode: errs.OK.String(),
		Data:      CursorData[T]{Items: items, Next: next, Prev: prev},
	}
}

// Encode implements the rest.ResponseEncoder interface.
func (r CursorResult[T]) Encode() ([]byte, string, error) {
	data, err := json.Marshal(r)
	return data, contentTypeJSON, err
}
