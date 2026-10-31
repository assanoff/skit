package sqlite

import (
	"cmp"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/assanoff/skit/dbx"
)

// DefaultDriver is the database/sql driver name Open uses when Config.Driver
// is empty: modernc.org/sqlite, the pure-Go driver (no cgo). The caller
// blank-imports it — dbx adds no SQLite driver dependency of its own.
const DefaultDriver = "sqlite"

// Defaults for a zero field of Config.
const (
	defaultMaxIdleConns = 2
	defaultBusyTimeout  = 5 * time.Second
)

// SQLite extended result codes the Classifier maps onto dbx.ErrDBDuplicatedEntry.
const (
	constraintPrimaryKey = 1555
	constraintUnique     = 2067
)

// Config configures a SQLite database opened by Open.
type Config struct {
	// Path is the database file, or ":memory:".
	Path string
	// Driver is the registered database/sql driver name; empty → DefaultDriver.
	Driver string
	// Pragmas applied through the modernc DSN (_pragma=…); ignored when DSN
	// is set.
	JournalMode string        // e.g. "WAL"; empty keeps the engine default
	BusyTimeout time.Duration // busy_timeout; zero keeps 5s
	ForeignKeys bool          // foreign_keys(1)
	// DSN, when set, is used verbatim as the driver DSN — for drivers whose
	// DSN syntax differs from modernc (e.g. mattn/go-sqlite3).
	DSN string
	// MaxOpenConns and MaxIdleConns bound the pool; zero leaves the open
	// connections unbounded and keeps 2 idle. An in-memory database
	// (":memory:" or mode=memory) keeps one connection by default: each
	// connection to it opens its own empty database.
	MaxOpenConns int
	MaxIdleConns int
}

// Open opens a SQLite pool through a driver the caller registered, bound to
// Classifier and to the sqlx bind style "sqlite3". It does not verify
// connectivity; call dbx.StatusCheck for that. A built DSN opens every
// transaction with BEGIN IMMEDIATE (_txlock=immediate), so a transaction that
// reads and then writes takes the write lock up front instead of failing with
// SQLITE_BUSY at its first write.
func Open(cfg Config) (*dbx.DB, error) {
	dsn := cfg.dsn()
	db, err := sql.Open(cmp.Or(cfg.Driver, DefaultDriver), dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open: %w", err)
	}
	if inMemory(dsn) {
		// One connection that never closes: the database lives as long as it
		// does, and every query sees the same one.
		db.SetMaxIdleConns(cmp.Or(cfg.MaxIdleConns, 1))
		db.SetMaxOpenConns(cmp.Or(cfg.MaxOpenConns, 1))
		return dbx.New(db, "sqlite3", Classifier{}), nil
	}
	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	db.SetMaxIdleConns(cmp.Or(cfg.MaxIdleConns, defaultMaxIdleConns))
	return dbx.New(db, "sqlite3", Classifier{}), nil
}

// inMemory reports whether dsn opens an in-memory SQLite database.
func inMemory(dsn string) bool {
	return strings.Contains(dsn, ":memory:") || strings.Contains(dsn, "mode=memory")
}

// dsn returns cfg.DSN verbatim when set, else builds a modernc.org/sqlite DSN
// (file:<path>?_txlock=immediate&_pragma=…) from Path and the pragma fields.
func (cfg Config) dsn() string {
	if cfg.DSN != "" {
		return cfg.DSN
	}
	busy := cmp.Or(cfg.BusyTimeout, defaultBusyTimeout)
	params := []string{
		"_txlock=immediate",
		fmt.Sprintf("_pragma=busy_timeout(%d)", busy.Milliseconds()),
	}
	if cfg.JournalMode != "" {
		params = append(params, "_pragma=journal_mode("+cfg.JournalMode+")")
	}
	if cfg.ForeignKeys {
		params = append(params, "_pragma=foreign_keys(1)")
	}
	return "file:" + cfg.Path + "?" + strings.Join(params, "&")
}

// coder is a driver error with a SQLite result code: the errors of
// modernc.org/sqlite.
type coder interface {
	error
	Code() int
}

// Classifier maps SQLite extended result codes onto the dbx sentinels: a
// unique (2067) or primary key (1555) constraint → dbx.ErrDBDuplicatedEntry.
// It reads the code through Code() int, which modernc.org/sqlite errors have,
// so it imports no driver; the errors of mattn/go-sqlite3 carry the code in a
// field and pass unchanged. It wraps the driver error, so errors.As still
// reaches it.
type Classifier struct{}

// Classify implements dbx.Classifier.
func (Classifier) Classify(err error) error {
	c, ok := errors.AsType[coder](err)
	if !ok {
		return err
	}
	switch c.Code() {
	case constraintUnique, constraintPrimaryKey:
		return fmt.Errorf("%w: %w", dbx.ErrDBDuplicatedEntry, err)
	}
	return err
}
