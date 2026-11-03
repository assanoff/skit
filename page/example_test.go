package page_test

import (
	"fmt"

	"github.com/assanoff/skit/page"
)

// Parse reads ?page= and ?rows= from a request; the store binds Offset and
// RowsPerPage. A bad value is an error the handler answers with 400.
func ExampleParse() {
	pg, err := page.Parse("3", "20")
	fmt.Println(pg.Number(), pg.RowsPerPage(), pg.Offset(), err)

	pg, err = page.Parse("", "")
	fmt.Println(pg.Number(), pg.RowsPerPage(), err)

	_, err = page.Parse("first", "")
	fmt.Println(err)
	// Output:
	// 3 20 40 <nil>
	// 1 10 <nil>
	// page conversion: strconv.Atoi: parsing "first": invalid syntax
}

// New takes trusted numbers, such as gRPC fields where unset reads 0: it
// falls back to the defaults and caps the rows instead of failing.
func ExampleNew() {
	pg := page.New(0, 500)
	fmt.Println(pg.Number(), pg.RowsPerPage())
	// Output:
	// 1 100
}

// MustParse builds a page from strings known to be good, such as in a test.
func ExampleMustParse() {
	fmt.Println(page.MustParse("2", "50"))
	// Output:
	// page: 2 rows: 50
}

// The store packs the key of the last row into the next cursor; the next
// request decodes it back. The first page has no token and an empty key.
func ExampleNewCursor() {
	next := page.EncodeCursor("2026-11-03T10:00:00Z|42")

	first := page.NewCursor("", 0)
	key, _ := first.Key()
	fmt.Printf("%q %d\n", key, first.Limit())

	c := page.NewCursor(next, 50)
	key, err := c.Key()
	fmt.Println(key, c.Limit(), err)
	// Output:
	// "" 10
	// 2026-11-03T10:00:00Z|42 50 <nil>
}
