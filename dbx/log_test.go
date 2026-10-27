package dbx

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"
	"github.com/matryer/is"
)

// execStub is a sqlx.ExtContext whose ExecContext returns err; the query
// methods are never reached by the exec helpers.
type execStub struct {
	sqlx.ExtContext

	err error
}

func (execStub) DriverName() string { return "pgx" }

func (execStub) Rebind(q string) string { return q }

func (s execStub) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, s.err
}

func jsonLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{AddSource: true}))
}

func TestExecLogsOnlyFailures(t *testing.T) {
	is := is.New(t)
	ctx := t.Context()
	var buf bytes.Buffer
	log := jsonLogger(&buf)

	is.NoErr(NamedExecContext(ctx, log, execStub{}, "UPDATE t SET a = 1", struct{}{}))
	is.Equal(buf.Len(), 0) // a successful statement is not logged

	boom := errors.New("boom")
	err := NamedExecContext(ctx, log, execStub{err: boom}, "UPDATE t SET a = 1", struct{}{})
	is.True(errors.Is(err, boom))

	var rec struct {
		Level  string
		Msg    string
		Query  string
		Err    string
		Source struct{ Function, File string }
	}
	is.NoErr(json.Unmarshal(buf.Bytes(), &rec))
	is.Equal(rec.Level, "INFO")
	is.Equal(rec.Msg, "dbx.exec")
	is.Equal(rec.Query, "UPDATE t SET a = 1")
	is.Equal(rec.Err, "boom")
	is.Equal(filepath.Base(rec.Source.File), "log_test.go") // the caller, not dbx
	is.Equal(rec.Source.Function, "github.com/assanoff/skit/dbx.TestExecLogsOnlyFailures")
}

func TestLogQuerySkipsNotFound(t *testing.T) {
	is := is.New(t)
	var buf bytes.Buffer

	err := logQuery(t.Context(), jsonLogger(&buf), 0, "dbx.query", "SELECT 1", nil, ErrDBNotFound)
	is.Equal(err, ErrDBNotFound)
	is.Equal(buf.Len(), 0) // "no rows" is an ordinary outcome
}

func TestExecDuplicateKeepsDriverError(t *testing.T) {
	is := is.New(t)
	var buf bytes.Buffer
	pgErr := &pgconn.PgError{Code: uniqueViolation, ConstraintName: "widgets_name_key"}

	err := NamedExecContext(t.Context(), jsonLogger(&buf), execStub{err: pgErr}, "INSERT INTO t VALUES (1)", struct{}{})
	is.True(errors.Is(err, ErrDBDuplicatedEntry))
	got, ok := errors.AsType[*pgconn.PgError](err)
	is.True(ok) // wrapped, not replaced
	is.Equal(got.ConstraintName, "widgets_name_key")
	is.Equal(buf.Len(), 0) // a taken key is the caller's to handle, not logged
}

// queryStub is a sqlx.ExtContext that records the query ExecContext gets.
type queryStub struct {
	execStub

	query *string
}

func (s queryStub) ExecContext(_ context.Context, q string, _ ...any) (sql.Result, error) {
	*s.query = q
	return driver.RowsAffected(0), nil
}

func TestExecContextRunsQueryAsWritten(t *testing.T) {
	is := is.New(t)
	var got string
	const q = `SELECT '{}'::jsonb, 'a:b' -- :note`

	is.NoErr(ExecContext(t.Context(), nil, queryStub{query: &got}, q))
	is.Equal(got, q) // no :name parsing: :: stays a cast, :b and :note text
}

func TestLogQueryNilLogger(t *testing.T) {
	is := is.New(t)
	boom := errors.New("boom")
	is.Equal(logQuery(t.Context(), nil, 0, "dbx.query", "SELECT 1", nil, boom), boom)
}
