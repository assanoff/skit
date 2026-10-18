package dbx

import (
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/matryer/is"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// openPool opens a pool of database name on host with metrics on reg; Open
// does not connect, so no server is needed.
func openPool(t *testing.T, reg prometheus.Registerer, host, name string) (*sqlx.DB, error) {
	t.Helper()
	db, err := Open(Config{User: "u", Host: host, Name: name, DisableTLS: true, Metrics: reg})
	if err == nil {
		t.Cleanup(func() { _ = db.Close() })
	}
	return db, err
}

// Each pool is its own series, labeled by database name and host.
func TestOpenRegistersPoolMetrics(t *testing.T) {
	is := is.New(t)
	reg := prometheus.NewRegistry()

	_, err := openPool(t, reg, "primary:5432", "app")
	is.NoErr(err)
	_, err = openPool(t, reg, "replica:5432", "app")
	is.NoErr(err) // a replica of the same database on another host

	n, err := testutil.GatherAndCount(reg, "go_sql_open_connections")
	is.NoErr(err)
	is.Equal(n, 2)
}

// A second pool of the same database on the same host and registry is a
// wiring error.
func TestOpenRejectsDuplicatePool(t *testing.T) {
	is := is.New(t)
	reg := prometheus.NewRegistry()
	_, err := openPool(t, reg, "primary:5432", "app")
	is.NoErr(err)

	db, err := openPool(t, reg, "primary:5432", "app")

	is.True(err != nil)
	is.True(db == nil)
}
