package dialect

import "bytes"

// SQLite is the Dialect for SQLite. It uses the LIMIT / OFFSET pagination form,
// which MySQL and modern Postgres also accept. It exists as a second,
// deliberately different dialect so the engine-specific seam stays visible and
// exercised.
type SQLite struct{}

// Name implements Dialect.
func (SQLite) Name() string { return "sqlite" }

// Paginate implements Dialect.
func (SQLite) Paginate(buf *bytes.Buffer) {
	buf.WriteString(" LIMIT :rows_per_page OFFSET :offset")
}
