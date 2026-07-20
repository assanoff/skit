package page

import (
	"fmt"
	"strconv"
)

const (
	// DefaultPageNumber is used when no page number is requested.
	DefaultPageNumber = 1
	// DefaultRowsPerPage is used when no page size is requested.
	DefaultRowsPerPage = 10
	// MaxRowsPerPage caps the page size to bound result sizes.
	MaxRowsPerPage = 100
)

// Page represents the requested page and rows per page.
type Page struct {
	number int
	rows   int
}

// Parse parses the strings (e.g. ?page= and ?rows= from untrusted client input)
// and validates the values are in reason. Empty strings fall back to the
// defaults; out-of-range or non-numeric values are errors a handler should map
// to a 400.
func Parse(page, rowsPerPage string) (Page, error) {
	number := DefaultPageNumber
	if page != "" {
		var err error
		number, err = strconv.Atoi(page)
		if err != nil {
			return Page{}, fmt.Errorf("page conversion: %w", err)
		}
	}

	rows := DefaultRowsPerPage
	if rowsPerPage != "" {
		var err error
		rows, err = strconv.Atoi(rowsPerPage)
		if err != nil {
			return Page{}, fmt.Errorf("rows conversion: %w", err)
		}
	}

	if number <= 0 {
		return Page{}, fmt.Errorf("page value too small, must be larger than 0")
	}

	if rows <= 0 {
		return Page{}, fmt.Errorf("rows value too small, must be larger than 0")
	}

	if rows > MaxRowsPerPage {
		return Page{}, fmt.Errorf("rows value too large, must be %d or less", MaxRowsPerPage)
	}

	return Page{number: number, rows: rows}, nil
}

// MustParse creates a paging value for testing; it panics on an invalid input.
func MustParse(page, rowsPerPage string) Page {
	pg, err := Parse(page, rowsPerPage)
	if err != nil {
		panic(err)
	}

	return pg
}

// New builds a Page from numeric values for trusted/optional inputs (e.g. gRPC
// fields where an unset value reads as 0, or programmatic callers). Unlike
// Parse, it is lenient: a non-positive number or rows-per-page falls back to the
// default, and rows above MaxRowsPerPage are capped rather than rejected.
func New(number, rowsPerPage int) Page {
	if number < 1 {
		number = DefaultPageNumber
	}

	switch {
	case rowsPerPage <= 0:
		rowsPerPage = DefaultRowsPerPage
	case rowsPerPage > MaxRowsPerPage:
		rowsPerPage = MaxRowsPerPage
	}

	return Page{number: number, rows: rowsPerPage}
}

// String implements the stringer interface.
func (p Page) String() string {
	return fmt.Sprintf("page: %d rows: %d", p.number, p.rows)
}

// Number returns the page number.
func (p Page) Number() int {
	return p.number
}

// RowsPerPage returns the rows per page.
func (p Page) RowsPerPage() int {
	return p.rows
}

// Offset returns the 0-based row offset for the page (used to bind a SQL
// :offset).
func (p Page) Offset() int {
	return (p.number - 1) * p.rows
}
