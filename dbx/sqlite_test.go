package dbx

import (
	"cmp"
	"database/sql"
	"database/sql/driver"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/matryer/is"
)

func TestSQLiteDSN(t *testing.T) {
	tests := []struct {
		name string
		cfg  SQLiteConfig
		want string
	}{
		{"bare path", SQLiteConfig{Path: "app.db"}, "file:app.db"},
		{"memory", SQLiteConfig{Path: ":memory:"}, "file::memory:"},
		{
			"pragmas",
			SQLiteConfig{Path: "app.db", JournalMode: "WAL", BusyTimeout: 5 * time.Second, ForeignKeys: true},
			"file:app.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)",
		},
		{"raw dsn override", SQLiteConfig{Path: "ignored", DSN: "file:x.db?_txlock=immediate"}, "file:x.db?_txlock=immediate"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.dsn(); got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

// A fake driver lets OpenSQLite be exercised without a real SQLite dependency:
// sqlx.Open is lazy, so the driver's Open is never called here.
type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) {
	return nil, driver.ErrBadConn
}

func TestOpenSQLiteUsesDriver(t *testing.T) {
	sql.Register("dbxfakesqlite", fakeDriver{})

	db, err := OpenSQLite(SQLiteConfig{Driver: "dbxfakesqlite", Path: ":memory:", MaxOpenConns: 1})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if db.DriverName() != "dbxfakesqlite" {
		t.Fatalf("driver = %q, want dbxfakesqlite", db.DriverName())
	}
}

// TestOpenSQLitePool checks the pool sizes: zero fields take the defaults, an
// in-memory database keeps one connection, the caller's sizes win.
func TestOpenSQLitePool(t *testing.T) {
	tests := []struct {
		name          string
		cfg           SQLiteConfig
		maxOpen, idle int
	}{
		{"file", SQLiteConfig{Path: "app.db"}, 0, 2},
		{"memory", SQLiteConfig{Path: ":memory:"}, 1, 1},
		{"memory dsn", SQLiteConfig{DSN: "file:x?mode=memory&cache=shared"}, 1, 1},
		{"caller sizes", SQLiteConfig{Path: "app.db", MaxOpenConns: 4, MaxIdleConns: 3}, 4, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			is := is.New(t)
			tc.cfg.Driver = tranDriverName()
			db, err := OpenSQLite(tc.cfg)
			is.NoErr(err)
			defer db.Close()

			is.Equal(db.Stats().MaxOpenConnections, tc.maxOpen)
			is.Equal(idleAfter(t, db, cmp.Or(tc.maxOpen, 3)), tc.idle)
		})
	}
}

// idleAfter takes n connections from db at once, returns them all and reports
// how many the pool keeps idle.
func idleAfter(t *testing.T, db *sqlx.DB, n int) int {
	t.Helper()
	conns := make([]*sql.Conn, n)
	for i := range conns {
		c, err := db.Conn(t.Context())
		if err != nil {
			t.Fatalf("conn: %v", err)
		}
		conns[i] = c
	}
	for _, c := range conns {
		_ = c.Close()
	}
	return db.Stats().Idle
}

// An empty Driver falls back to the modernc "sqlite" name; without that driver
// registered, sqlx reports it — proving the default is wired.
func TestOpenSQLiteDefaultDriver(t *testing.T) {
	_, err := OpenSQLite(SQLiteConfig{Path: ":memory:"})
	if err == nil || !strings.Contains(err.Error(), DefaultSQLiteDriver) {
		t.Fatalf("want unknown-driver error mentioning %q, got %v", DefaultSQLiteDriver, err)
	}
}
