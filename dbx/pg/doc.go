// Package pg opens a Postgres pool for dbx through the pgx driver, with query
// tracing, pool metrics and a Classifier of Postgres error codes.
//
// # Usage
//
//	db, err := pg.Open(pg.Config{
//	    User: "postgres", Password: "postgres",
//	    Host: "localhost:5432", Name: "app",
//	    DisableTLS: true,
//	    Metrics: reg, TracerProvider: tp,
//	})
//	if err != nil {
//	    return err
//	}
//	defer db.Close()
//	if err := dbx.StatusCheck(ctx, db); err != nil {
//	    return err
//	}
//
// The returned *dbx.DB runs every dbx helper; its errors are classified by
// Classifier: a unique violation (23505) wraps dbx.ErrDBDuplicatedEntry, a
// deadlock (40P01) or a serialization failure (40001) wraps
// dbx.ErrDBRetryable, on which dbx.WithinTran re-runs the transaction. The
// driver error stays reachable through errors.As, with its constraint and
// detail. Classifier also serves a pool opened another way, such as through
// lib/pq: dbx.New(db, "postgres", pg.Classifier{}).
//
// # Metrics
//
// With Config.Metrics set, Open registers the pool metrics there: go_sql_*
// (open, in-use and idle connections, waits, closes) labeled db_name with
// Config.Name and host with Config.Host, so a replica on another host is its
// own series. They read the pool stats on each scrape, without a query. A
// second pool of the same Host and Name on one registry is an error of Open.
//
// # Tracing
//
// Open records every query of the pool as a CLIENT span on
// Config.TracerProvider, named after the database (Config.Name). BEGIN and
// COMMIT are queries too, so a transaction shows as its own spans;
// dbx.StatusCheck pings past the tracer. The span carries db.system.name,
// db.namespace, server.address, server.port and db.query.text: the query as
// written, cut to 2 KiB. The arguments are never recorded — pass values as
// arguments, never in the query text. A Postgres error sets
// db.response.status_code and error.type to its SQLSTATE; no rows is not an
// error. A query from a suppressed context (otel.Suppress, the ticks of
// worker.Loop) records nothing.
//
// # Config
//
// Config fields: User, Password, Host, Name, Schema (sets search_path),
// MaxIdleConns and MaxOpenConns (zero keeps 5 idle and 20 open connections),
// ConnMaxLifetime and ConnMaxIdleTime (zero keeps 1h and 30m, as pgxpool),
// DisableTLS (true selects sslmode=disable, otherwise sslmode=require),
// Metrics (the registry of the pool metrics; nil disables them),
// TracerProvider (nil → the global provider). Open always sets timezone=utc.
package pg
