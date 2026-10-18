package dbx

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/matryer/is"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// traceQuery runs one query through a tracer of the database app on db:5432
// and returns its span.
func traceQuery(t *testing.T, sql string, args []any, err error) sdktrace.ReadOnlySpan {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	qt := newQueryTracer(tp, &pgx.ConnConfig{Host: "db", Port: 5432, Database: "app"})

	ctx := qt.TraceQueryStart(t.Context(), nil, pgx.TraceQueryStartData{SQL: sql, Args: args})
	qt.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: err})

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	return spans[0]
}

// attrs returns the attributes of span by key.
func attrs(span sdktrace.ReadOnlySpan) map[attribute.Key]string {
	out := make(map[attribute.Key]string)
	for _, kv := range span.Attributes() {
		out[kv.Key] = kv.Value.String()
	}
	return out
}

// A query is a CLIENT span named after the database, with its text and
// without its arguments.
func TestQueryTracerRecordsQuery(t *testing.T) {
	is := is.New(t)
	const sql = `SELECT id FROM widgets WHERE name = $1`

	span := traceQuery(t, sql, []any{"secret"}, nil)

	is.Equal(span.Name(), "app")
	is.Equal(span.SpanKind(), trace.SpanKindClient)
	is.Equal(span.Status().Code, codes.Unset)
	is.Equal(attrs(span), map[attribute.Key]string{
		"db.system.name": "postgresql",
		"db.namespace":   "app",
		"server.address": "db",
		"server.port":    "5432",
		"db.query.text":  sql,
	})
}

// A Postgres error marks the span failed with its SQLSTATE.
func TestQueryTracerRecordsSQLState(t *testing.T) {
	is := is.New(t)
	err := &pgconn.PgError{Code: "23505", Message: "duplicate key value"}

	span := traceQuery(t, `INSERT INTO widgets (id) VALUES ($1)`, nil, err)

	is.Equal(span.Status().Code, codes.Error)
	is.Equal(attrs(span)["db.response.status_code"], "23505")
	is.Equal(attrs(span)["error.type"], "23505")
}

// Another error marks the span failed without a status code.
func TestQueryTracerRecordsError(t *testing.T) {
	is := is.New(t)

	span := traceQuery(t, `SELECT 1`, nil, context.DeadlineExceeded)

	is.Equal(span.Status().Code, codes.Error)
	_, ok := attrs(span)["db.response.status_code"]
	is.True(!ok)
	is.True(attrs(span)["error.type"] != "")
}

// A long query is cut to maxQueryText bytes of valid UTF-8.
func TestQueryTracerCutsLongText(t *testing.T) {
	is := is.New(t)
	sql := strings.Repeat("a", maxQueryText-1) + "ж" // the cut splits "ж"

	text := attrs(traceQuery(t, sql, nil, nil))["db.query.text"]

	is.Equal(text, strings.Repeat("a", maxQueryText-1))
	is.True(utf8.ValidString(text))
}

// Without a database the span is named after the system.
func TestQueryTracerNameWithoutDatabase(t *testing.T) {
	is := is.New(t)

	qt := newQueryTracer(nil, &pgx.ConnConfig{})

	is.Equal(qt.name, "postgresql")
}

func BenchmarkQueryTracer(b *testing.B) {
	for _, bc := range []struct {
		name    string
		sampler sdktrace.Sampler
	}{
		{"recording", sdktrace.AlwaysSample()},
		{"dropped", sdktrace.NeverSample()},
	} {
		b.Run(bc.name, func(b *testing.B) {
			tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(bc.sampler))
			qt := newQueryTracer(tp, &pgx.ConnConfig{Host: "db", Port: 5432, Database: "app"})
			start := pgx.TraceQueryStartData{SQL: `SELECT id FROM widgets WHERE name = $1`, Args: []any{"a"}}
			b.ReportAllocs()
			for b.Loop() {
				ctx := qt.TraceQueryStart(b.Context(), nil, start)
				qt.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
			}
		})
	}
}
