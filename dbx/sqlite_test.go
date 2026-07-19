package dbx

import (
	"database/sql"
	"database/sql/driver"
	"strings"
	"testing"
	"time"
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

func (fakeDriver) Open(string) (driver.Conn, error) { return nil, driver.ErrBadConn }

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

// An empty Driver falls back to the modernc "sqlite" name; without that driver
// registered, sqlx reports it — proving the default is wired.
func TestOpenSQLiteDefaultDriver(t *testing.T) {
	_, err := OpenSQLite(SQLiteConfig{Path: ":memory:"})
	if err == nil || !strings.Contains(err.Error(), DefaultSQLiteDriver) {
		t.Fatalf("want unknown-driver error mentioning %q, got %v", DefaultSQLiteDriver, err)
	}
}
