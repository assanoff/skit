package dbx

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"
	"github.com/matryer/is"
)

// execStub is a sqlx.ExtContext whose ExecContext records the query and
// returns err; the query methods are never reached by the exec helpers.
type execStub struct {
	sqlx.ExtContext

	query *string
	err   error
}

func (execStub) DriverName() string { return "pgx" }

func (execStub) Rebind(q string) string { return q }

func (s execStub) ExecContext(_ context.Context, q string, _ ...any) (sql.Result, error) {
	*s.query = q
	return driver.RowsAffected(0), s.err
}

func TestExecRunsQueryAsWritten(t *testing.T) {
	is := is.New(t)
	var got string
	const q = `SELECT '{}'::jsonb, 'a:b' -- :note`

	is.NoErr(Exec(t.Context(), execStub{query: &got}, q))
	is.Equal(got, q) // no :name parsing: :: stays a cast, :b and :note text
}

func TestExecDuplicateKeepsDriverError(t *testing.T) {
	is := is.New(t)
	var got string
	pgErr := &pgconn.PgError{Code: uniqueViolation, ConstraintName: "widgets_name_key"}

	err := NamedExec(t.Context(), execStub{query: &got, err: pgErr}, "INSERT INTO t VALUES (1)", struct{}{})
	is.True(errors.Is(err, ErrDBDuplicatedEntry))
	wrapped, ok := errors.AsType[*pgconn.PgError](err)
	is.True(ok) // wrapped, not replaced
	is.Equal(wrapped.ConstraintName, "widgets_name_key")
}
