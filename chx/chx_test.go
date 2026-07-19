package chx

import (
	"context"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

func TestConfigOptions(t *testing.T) {
	// Defaults: dial timeout falls back to 10s, no compression.
	o := Config{Addrs: []string{"h:9000"}, Database: "db"}.options()
	if o.DialTimeout != 10*time.Second {
		t.Fatalf("dial timeout = %v, want 10s default", o.DialTimeout)
	}
	if o.Compression != nil {
		t.Fatalf("compression should be nil by default")
	}
	if o.Auth.Database != "db" {
		t.Fatalf("auth db = %q", o.Auth.Database)
	}

	// Compression enabled -> LZ4.
	o = Config{Addrs: []string{"h:9000"}, Compression: true, DialTimeout: time.Second}.options()
	if o.Compression == nil || o.Compression.Method != clickhouse.CompressionLZ4 {
		t.Fatalf("compression = %+v, want LZ4", o.Compression)
	}
	if o.DialTimeout != time.Second {
		t.Fatalf("dial timeout override = %v", o.DialTimeout)
	}
}

// TestMigrateAndRoundTripLive applies a migration from an in-memory FS and does a
// batch-insert/select round trip against a real ClickHouse. Needs
// SKIT_CHX_TEST_ADDR (e.g. localhost:9000); skips otherwise.
func TestMigrateAndRoundTripLive(t *testing.T) {
	addr := os.Getenv("SKIT_CHX_TEST_ADDR")
	if addr == "" {
		t.Skip("SKIT_CHX_TEST_ADDR not set; skipping ClickHouse integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const dbName = "skit_chx_test"
	boot, err := clickhouse.Open(&clickhouse.Options{Addr: []string{addr}})
	if err != nil {
		t.Fatalf("bootstrap open: %v", err)
	}
	if err := boot.Exec(ctx, "CREATE DATABASE IF NOT EXISTS "+dbName); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		_ = boot.Exec(context.Background(), "DROP DATABASE IF EXISTS "+dbName)
		_ = boot.Close()
	})

	cfg := Config{Addrs: []string{addr}, Database: dbName}

	migrations := fstest.MapFS{
		"00001_kv.sql": &fstest.MapFile{Data: []byte(`-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE IF NOT EXISTS kv (k String, v Int64) ENGINE = MergeTree ORDER BY k;
-- +goose Down
DROP TABLE IF EXISTS kv;
`)},
	}
	if err := Migrate(ctx, cfg, migrations); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cl, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = cl.Close() })

	batch, err := cl.PrepareBatch(ctx, "INSERT INTO kv")
	if err != nil {
		t.Fatalf("prepare batch: %v", err)
	}
	if err := batch.Append("a", int64(1)); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := batch.Append("b", int64(2)); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := batch.Send(); err != nil {
		t.Fatalf("send: %v", err)
	}

	var out []struct {
		K string `ch:"k"`
		V int64  `ch:"v"`
	}
	if err := cl.Select(ctx, &out, "SELECT k, v FROM kv ORDER BY k"); err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(out) != 2 || out[0].K != "a" || out[0].V != 1 || out[1].V != 2 {
		t.Fatalf("round trip = %+v, want a=1 b=2", out)
	}
}
