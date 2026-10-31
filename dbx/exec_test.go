package dbx

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/matryer/is"
)

// execStub is a Querier whose ExecContext records the query and returns err;
// the query methods are never reached by the exec helpers.
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

func (execStub) within(context.Context, *slog.Logger, func(tx *Tx) error) error {
	return errors.New("not implemented")
}

func (execStub) classify(err error) error {
	return classify(dupClassifier{}, err)
}

// errTaken stands for a driver error of a unique key that dupClassifier
// marks a duplicate.
var errTaken = errors.New("unique violation")

type dupClassifier struct{}

func (dupClassifier) Classify(err error) error {
	if errors.Is(err, errTaken) {
		return fmt.Errorf("%w: %w", ErrDBDuplicatedEntry, err)
	}
	return err
}

func TestExecRunsQueryAsWritten(t *testing.T) {
	is := is.New(t)
	var got string
	const q = `SELECT '{}'::jsonb, 'a:b' -- :note`

	is.NoErr(Exec(t.Context(), execStub{query: &got}, q))
	is.Equal(got, q) // no :name parsing: :: stays a cast, :b and :note text
}

func TestExecClassifiesDriverError(t *testing.T) {
	is := is.New(t)
	var got string
	driverErr := fmt.Errorf("widgets_name_key: %w", errTaken)

	err := NamedExec(t.Context(), execStub{query: &got, err: driverErr}, "INSERT INTO t VALUES (1)", struct{}{})
	is.True(errors.Is(err, ErrDBDuplicatedEntry))
	is.True(errors.Is(err, driverErr)) // wrapped, not replaced
}
