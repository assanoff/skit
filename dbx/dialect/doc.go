// Package dialect provides cross-cutting helpers that vary between SQL engines
// but are otherwise reusable by every domain store. A store wires in one
// implementation (Postgres, SQLite, ...) and delegates the small set of
// engine-specific decisions to it, so shared SQL fragments stay portable.
//
// The set of decisions is intentionally narrow — only add to the interface when
// a real, second engine forces a difference.
//
// # Usage
//
//	d := dialect.Postgres{} // or dialect.SQLite{}, chosen per store
//
//	var buf bytes.Buffer
//	buf.WriteString("SELECT id, name FROM widgets ORDER BY id")
//	d.Paginate(&buf) // appends the engine's pagination clause
//
//	// Bind :offset and :rows_per_page in the params passed to
//	// dbx.NamedQuerySlice / NamedQueryStruct.
//	params := map[string]any{"offset": 0, "rows_per_page": 50}
//
// # Implementations
//
//   - Postgres: SQL:2008 " OFFSET :offset ROWS FETCH NEXT :rows_per_page ROWS ONLY".
//   - SQLite:   " LIMIT :rows_per_page OFFSET :offset" (also valid for MySQL).
package dialect
