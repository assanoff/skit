// Package page provides a small, validated value for query paging, shared by
// the transport (parsing ?page=/?rows= or gRPC fields) and the store (binding
// :offset / :rows_per_page, e.g. with dbx/dialect.Paginate).
//
// # Usage
//
//	// REST: parse query params (non-numeric -> error -> 400).
//	pg, err := page.Parse(r.URL.Query().Get("page"), r.URL.Query().Get("rows"))
//
//	// gRPC / programmatic: clamp ints (0/unset -> first default page).
//	pg := page.New(int(req.GetPage()), int(req.GetPageSize()))
//
//	// store: bind the SQL paging parameters.
//	data := map[string]any{"offset": pg.Offset(), "rows_per_page": pg.RowsPerPage()}
//
// New clamps the number to >= 1 and rows-per-page into [1, MaxRowsPerPage],
// defaulting non-positive rows to DefaultRowsPerPage. Pair the result with
// query.Result to return a paginated list envelope.
//
// # Cursor paging
//
// Cursor (keyset) paging is also supported via Cursor: an opaque token plus a
// limit. The SDK owns the token (EncodeCursor / Cursor.Key); the store builds
// the engine-specific keyset predicate and encodes the boundary row's key into
// the next cursor. Pair it with query.CursorResult.
//
//	c := page.NewCursor(r.URL.Query().Get("cursor"), 50)
//	key, _ := c.Key()                 // "" on the first page
//	// store: WHERE (created_at, id) < decode(key) ORDER BY ... LIMIT c.Limit()
//	next := page.EncodeCursor(lastRowKey) // "" when the page wasn't full
package page
