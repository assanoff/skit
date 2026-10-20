package chx

import (
	"testing"
	"testing/fstest"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/matryer/is"
	"github.com/testcontainers/testcontainers-go"
	tcclickhouse "github.com/testcontainers/testcontainers-go/modules/clickhouse"
)

func TestConfigOptions(t *testing.T) {
	is := is.New(t)

	o := Config{Addrs: []string{"h:9000"}, Database: "db"}.options()
	is.Equal(o.DialTimeout, 10*time.Second) // the dial timeout defaults to 10s
	is.True(o.Compression == nil)           // no compression by default
	is.Equal(o.Auth.Database, "db")

	o = Config{
		Addrs:       []string{"h:9000"},
		Compression: true,
		DialTimeout: time.Second,
	}.options()
	is.True(o.Compression != nil)                             // compression must be on
	is.Equal(o.Compression.Method, clickhouse.CompressionLZ4) // with LZ4
	is.Equal(o.DialTimeout, time.Second)                      // the set timeout wins
}

// kv is a row of the table the test migration creates.
type kv struct {
	K string `ch:"k"`
	V int64  `ch:"v"`
}

// TestMigrateAndRoundTrip applies a migration from an in-memory FS, then
// batch-inserts two rows and selects them back from a real ClickHouse.
func TestMigrateAndRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires docker")
	}

	is := is.New(t)
	ctx := t.Context()
	cfg := startClickHouse(t)

	migrations := fstest.MapFS{
		"00001_kv.sql": {Data: []byte(`-- +goose Up
CREATE TABLE kv (k String, v Int64) ENGINE = MergeTree ORDER BY k;
-- +goose Down
DROP TABLE kv;
`)},
	}
	is.NoErr(Migrate(ctx, cfg, migrations)) // the migration must apply
	is.NoErr(Migrate(ctx, cfg, migrations)) // a second run must be a no-op

	cl, err := Open(ctx, cfg)
	is.NoErr(err) // the client must connect
	t.Cleanup(func() { _ = cl.Close() })
	is.NoErr(cl.Ping(ctx))

	batch, err := cl.PrepareBatch(ctx, "INSERT INTO kv")
	is.NoErr(err)
	is.NoErr(batch.Append("a", int64(1)))
	is.NoErr(batch.Append("b", int64(2)))
	is.NoErr(batch.Send()) // the batch must land

	var got []kv
	is.NoErr(cl.Select(ctx, &got, "SELECT k, v FROM kv ORDER BY k"))
	is.Equal(got, []kv{{K: "a", V: 1}, {K: "b", V: 2}})
}

// startClickHouse runs a ClickHouse container for the test and returns the
// Config pointing at it. The container is removed when the test ends.
func startClickHouse(t *testing.T) Config {
	t.Helper()
	is := is.New(t)

	cfg := Config{Database: "skit", Username: "skit", Password: "skit"}
	c, err := tcclickhouse.Run(t.Context(),
		"clickhouse/clickhouse-server:25.8-alpine",
		tcclickhouse.WithDatabase(cfg.Database),
		tcclickhouse.WithUsername(cfg.Username),
		tcclickhouse.WithPassword(cfg.Password),
	)
	// Run may return a created container along with the error.
	testcontainers.CleanupContainer(t, c)
	is.NoErr(err) // the container must start

	addr, err := c.ConnectionHost(t.Context())
	is.NoErr(err)
	cfg.Addrs = []string{addr}
	return cfg
}
