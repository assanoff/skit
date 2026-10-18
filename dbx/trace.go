package dbx

import (
	"cmp"
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/assanoff/skit/otel"
)

// scope names the tracer of this package.
const scope = "github.com/assanoff/skit/dbx"

// maxQueryText bounds db.query.text: a bulk insert of many rows must not turn
// into an attribute the size of the statement.
const maxQueryText = 2048

// queryTracer records every query of a pgx connection as a CLIENT span. It
// sees what pgx runs through Query, QueryRow and Exec, BEGIN and COMMIT
// included; a ping goes past it.
type queryTracer struct {
	tracer trace.Tracer
	// name is the span name: the database, else the system.
	name  string
	start []trace.SpanStartOption
}

// Compile-time check.
var _ pgx.QueryTracer = (*queryTracer)(nil)

// newQueryTracer builds the tracer of the connections of cfg.
func newQueryTracer(tp trace.TracerProvider, cfg *pgx.ConnConfig) *queryTracer {
	attrs := []attribute.KeyValue{
		semconv.DBSystemNamePostgreSQL,
		semconv.ServerAddress(cfg.Host),
		semconv.ServerPort(int(cfg.Port)),
	}
	if cfg.Database != "" {
		attrs = append(attrs, semconv.DBNamespace(cfg.Database))
	}
	return &queryTracer{
		tracer: otel.Tracer(tp, scope),
		name:   cmp.Or(cfg.Database, "postgresql"),
		start: []trace.SpanStartOption{
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(attrs...),
		},
	}
}

// TraceQueryStart implements pgx.QueryTracer: it opens the span of the query.
// The query text goes into db.query.text as written, the arguments never do.
func (t *queryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx, span := t.tracer.Start(ctx, t.name, t.start...)
	if span.IsRecording() {
		span.SetAttributes(semconv.DBQueryText(queryText(data.SQL)))
	}
	return ctx
}

// TraceQueryEnd implements pgx.QueryTracer: it ends the span of the query. A
// Postgres error sets db.response.status_code and error.type to its SQLSTATE.
// No rows is not an error here: pgx and database/sql report it after the query
// ends.
func (t *queryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	span := trace.SpanFromContext(ctx)
	defer span.End()
	if data.Err == nil {
		return
	}
	otel.SetError(span, data.Err)
	if pgErr, ok := errors.AsType[*pgconn.PgError](data.Err); ok {
		span.SetAttributes(semconv.DBResponseStatusCode(pgErr.Code), semconv.ErrorTypeKey.String(pgErr.Code))
	}
}

// queryText cuts sql to maxQueryText bytes, dropping a rune the cut split.
func queryText(sql string) string {
	if len(sql) <= maxQueryText {
		return sql
	}
	return strings.ToValidUTF8(sql[:maxQueryText], "")
}
