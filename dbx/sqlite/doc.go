// Package sqlite opens a SQLite pool for dbx through a driver the caller
// registers, modernc.org/sqlite by default, with a Classifier of SQLite result
// codes.
//
// # Usage
//
//	import _ "modernc.org/sqlite"
//
//	db, err := sqlite.Open(sqlite.Config{Path: "app.db", JournalMode: "WAL"})
//	if err != nil {
//	    return err
//	}
//	defer db.Close()
//
// The returned *dbx.DB runs every dbx helper with the sqlx bind style
// "sqlite3", so named queries bind as ?. Its errors are classified by
// Classifier: a unique (2067) or primary key (1555) constraint wraps
// dbx.ErrDBDuplicatedEntry; the driver error stays reachable through
// errors.As.
//
// # Transactions
//
// A DSN built from Config opens every transaction with BEGIN IMMEDIATE
// (_txlock=immediate) and waits for a lock up to BusyTimeout. A transaction
// that reads and then writes therefore takes the write lock up front instead
// of failing with SQLITE_BUSY at its first write when another one writes
// meanwhile. A DSN of your own (Config.DSN) sets neither.
//
// # Pitfalls
//
//   - Time is stored as text. A column filled both by a DEFAULT and from Go
//     holds two formats, which compare as strings: do not page by it.
//
// # Config
//
// Config fields: Path (a file or ":memory:"), Driver (empty → DefaultDriver,
// "sqlite"), JournalMode, BusyTimeout (zero keeps 5s), ForeignKeys, DSN (used
// verbatim; the other DSN fields are ignored), MaxOpenConns and MaxIdleConns
// (zero leaves the open connections unbounded and keeps 2 idle). An in-memory
// database keeps one connection that never closes by default: each
// connection to it opens its own empty database.
package sqlite
