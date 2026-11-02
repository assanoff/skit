package pg_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/matryer/is"

	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/dbx/pg"
)

func TestClassify(t *testing.T) {
	is := is.New(t)
	classify := pg.Classify

	dup := &pgconn.PgError{Code: "23505", ConstraintName: "widgets_name_key"}
	err := classify(fmt.Errorf("insert: %w", dup))
	is.True(errors.Is(err, dbx.ErrDBDuplicatedEntry))
	got, ok := errors.AsType[*pgconn.PgError](err)
	is.True(ok) // wrapped, not replaced
	is.Equal(got.ConstraintName, "widgets_name_key")

	is.True(errors.Is(classify(&pgconn.PgError{Code: "40P01"}), dbx.ErrDBRetryable)) // deadlock
	is.True(errors.Is(classify(&pgconn.PgError{Code: "40001"}), dbx.ErrDBRetryable)) // serialization

	other := &pgconn.PgError{Code: "42P01"}
	is.Equal(classify(other), error(other)) // unknown code: unchanged
	boom := errors.New("boom")
	is.Equal(classify(boom), boom) // not a Postgres error: unchanged
}
