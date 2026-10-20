// Package chx is a thin ClickHouse client for analytics and long-term stores.
//
// The write path uses the native clickhouse-go/v2 connection (driver.Conn),
// which speaks the columnar native protocol and supports PrepareBatch for large
// bulk inserts. Migrate applies the schema with skit/migrate (goose, ClickHouse
// dialect) from a migrations fs.FS the caller supplies, over a separate
// database/sql handle, because goose needs database/sql rather than the native
// conn.
//
// ClickHouse suits analytics and long-term storage: finished, structured data
// in large batches, never a hot ingest path. Importing this package pulls in
// the clickhouse-go/v2 dependency; wire it in only when a ClickHouse endpoint is
// configured.
//
// # Usage
//
// Apply the migrations once at startup, open the client, and write in batches:
//
//	//go:embed migrations/*.sql
//	var migrationsFS embed.FS
//
//	cfg := chx.Config{Addrs: []string{"clickhouse:9000"}, Database: "analytics"}
//	migrations, err := fs.Sub(migrationsFS, "migrations")
//	if err != nil { ... }
//	if err := chx.Migrate(ctx, cfg, migrations); err != nil { ... }
//
//	cl, err := chx.Open(ctx, cfg)
//	if err != nil { ... }
//	defer cl.Close()
//
//	batch, err := cl.PrepareBatch(ctx, "INSERT INTO events")
//	if err != nil { ... }
//	for _, e := range events {
//	    if err := batch.Append(e.ID, e.Name, e.At); err != nil { ... }
//	}
//	err = batch.Send()
//
// Open does not migrate: schema changes stay an intentional step at startup,
// not a side effect of connecting. Conn exposes the native driver.Conn for
// anything the pass-throughs (Exec, Select, Query, PrepareBatch) do not cover.
//
// # Config
//
//   - Addrs: native-protocol endpoints, host:port (ClickHouse listens on 9000).
//   - Database, Username, Password: the database and the credentials.
//   - DialTimeout: bound on dialing a connection (default 10s).
//   - MaxOpenConns: pool size (0 = the driver default, 10).
//   - Compression: LZ4 wire compression, worthwhile for large batches.
package chx
