package settings_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/matryer/is"

	"github.com/assanoff/skit/dbtest"
	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/settings"
)

// defaults are the limits before the first Save.
var defaults = Limits{MaxWithdrawal: 100}

// startDB starts Postgres in a container and creates the table settings.
func startDB(t *testing.T) *dbx.DB {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test requires docker")
	}
	t.Parallel()
	db := dbtest.NewPostgres(t.Context(), t, dbtest.Config{}).DB
	if err := settings.EnsureSchema(t.Context(), nil, db); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}
	return db
}

// open reads the limits stored under name.
func open(t *testing.T, db *dbx.DB, name string, opts ...settings.Option) *settings.Settings[Limits] {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	s, err := settings.New(t.Context(), log, db, name, defaults, opts...)
	if err != nil {
		t.Fatalf("New(%s) error = %v", name, err)
	}
	return s
}

// The settings are the defaults until the first Save; a stored document is
// decoded over them: a field it lacks keeps its default, a member Limits lacks
// is skipped.
func TestNew(t *testing.T) {
	is := is.New(t)
	db := startDB(t)

	is.Equal(open(t, db, "limits").Current(), defaults) // nothing stored

	const old = `INSERT INTO settings (name, value)
		VALUES ('old', '{"maintenance": true, "removed": 1}')`
	is.NoErr(dbx.Exec(t.Context(), db, old))
	is.Equal(open(t, db, "old").Current(), Limits{MaxWithdrawal: 100, Maintenance: true})
}

// A Save shows on the replica that made it at once, on another one with its
// next reread, and a replica that starts later reads it.
func TestSave(t *testing.T) {
	is := is.New(t)
	db := startDB(t)
	a := open(t, db, "limits")
	b := open(t, db, "limits", settings.WithInterval(10*time.Millisecond))

	saved := Limits{MaxWithdrawal: 500, Maintenance: true}
	is.NoErr(a.Save(t.Context(), saved))
	is.Equal(a.Current(), saved)                       // at once
	is.Equal(b.Current(), defaults)                    // not yet
	is.Equal(open(t, db, "limits").Current(), saved)   // a new replica reads it
	is.Equal(open(t, db, "other").Current(), defaults) // settings of another name

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- b.Start(ctx) }()
	deadline := time.After(10 * time.Second)
	for b.Current() != saved {
		select {
		case <-deadline:
			t.Fatalf("b.Current() = %+v, want %+v", b.Current(), saved)
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	is.True(errors.Is(<-done, context.Canceled)) // Start returns on cancel
	is.NoErr(b.Stop(t.Context()))
	is.Equal(b.Name(), "settings-limits")
}

// Without the table New fails, and so does a Save, which keeps the settings
// as they were.
func TestNoTable(t *testing.T) {
	is := is.New(t)
	db := startDB(t)
	s := open(t, db, "limits")
	is.NoErr(dbx.Exec(t.Context(), db, "DROP TABLE settings"))

	is.True(s.Save(t.Context(), Limits{MaxWithdrawal: 500}) != nil)
	is.Equal(s.Current(), defaults) // the settings stay

	_, err := settings.New(t.Context(), nil, db, "limits", defaults)
	is.True(err != nil) // the service does not start on the defaults
}

// New checks its name and its defaults before it reads.
func TestNewChecks(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	_, err := settings.New(ctx, nil, nil, "", defaults)
	is.True(err != nil) // no name

	type broken struct{ F func() }
	_, err = settings.New(ctx, nil, nil, "broken", broken{})
	is.True(err != nil) // defaults that are not JSON
}
