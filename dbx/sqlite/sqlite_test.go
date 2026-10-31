package sqlite

import (
	"cmp"
	"database/sql"
	"database/sql/driver"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/matryer/is"

	"github.com/assanoff/skit/dbx"
)

func TestDSN(t *testing.T) {
	const base = "_txlock=immediate&_pragma=busy_timeout(5000)"
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{"bare path", Config{Path: "app.db"}, "file:app.db?" + base},
		{"memory", Config{Path: ":memory:"}, "file::memory:?" + base},
		{
			"pragmas",
			Config{Path: "app.db", JournalMode: "WAL", BusyTimeout: time.Second, ForeignKeys: true},
			"file:app.db?_txlock=immediate&_pragma=busy_timeout(1000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)",
		},
		{"raw dsn override", Config{Path: "ignored", DSN: "file:x.db?mode=ro"}, "file:x.db?mode=ro"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.dsn(); got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

// fakeDriver is a database/sql driver whose connections only open and close —
// enough to size the pool without SQLite.
type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) { return fakeConn{}, nil }

type fakeConn struct{}

func (fakeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not implemented")
}
func (fakeConn) Close() error { return nil }
func (fakeConn) Begin() (driver.Tx, error) {
	return nil, errors.New("not implemented")
}

// fakeDriverName registers fakeDriver once and returns its name.
func fakeDriverName() string {
	const name = "dbxfakesqlite"
	if !slices.Contains(sql.Drivers(), name) {
		sql.Register(name, fakeDriver{})
	}
	return name
}

// TestOpenPool checks the pool sizes: zero fields take the defaults, an
// in-memory database keeps one connection, the caller's sizes win.
func TestOpenPool(t *testing.T) {
	tests := []struct {
		name          string
		cfg           Config
		maxOpen, idle int
	}{
		{"file", Config{Path: "app.db"}, 0, 2},
		{"memory", Config{Path: ":memory:"}, 1, 1},
		{"memory dsn", Config{DSN: "file:x?mode=memory&cache=shared"}, 1, 1},
		{"caller sizes", Config{Path: "app.db", MaxOpenConns: 4, MaxIdleConns: 3}, 4, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			is := is.New(t)
			tc.cfg.Driver = fakeDriverName()
			db, err := Open(tc.cfg)
			is.NoErr(err)
			defer db.Close()

			is.Equal(db.DriverName(), "sqlite3") // the sqlx bind style
			is.Equal(db.Stats().MaxOpenConnections, tc.maxOpen)
			is.Equal(idleAfter(t, db, cmp.Or(tc.maxOpen, 3)), tc.idle)
		})
	}
}

// idleAfter takes n connections from db at once, returns them all and reports
// how many the pool keeps idle.
func idleAfter(t *testing.T, db *dbx.DB, n int) int {
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

// An unregistered driver name is an error of Open that names the driver.
func TestOpenUnknownDriver(t *testing.T) {
	_, err := Open(Config{Driver: "nosuchsqlite", Path: ":memory:"})
	if err == nil || !strings.Contains(err.Error(), "nosuchsqlite") {
		t.Fatalf("want unknown-driver error mentioning nosuchsqlite, got %v", err)
	}
}
