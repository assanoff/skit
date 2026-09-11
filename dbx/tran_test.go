package dbx

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"
)

// tranDriver is a database/sql driver whose connections only know how to begin,
// commit and roll back — enough to drive WithinTran without a database.
type tranDriver struct{}

func (tranDriver) Open(string) (driver.Conn, error) { return tranConn{}, nil }

type tranConn struct{}

func (tranConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not implemented") }
func (tranConn) Close() error                        { return nil }
func (tranConn) Begin() (driver.Tx, error)           { return tranTx{}, nil }

type tranTx struct{}

func (tranTx) Commit() error   { return nil }
func (tranTx) Rollback() error { return nil }

func openTranDB(t *testing.T) *sqlx.DB {
	t.Helper()
	const name = "dbxtrandriver"
	registered := false
	for _, d := range sql.Drivers() {
		if d == name {
			registered = true
		}
	}
	if !registered {
		sql.Register(name, tranDriver{})
	}
	db, err := sqlx.Open(name, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func deadlockErr() error {
	return fmt.Errorf("namedexec: %w", &pgconn.PgError{Code: deadlockDetected, Message: "deadlock detected"})
}

// noDelay makes the retries instant for the duration of the test.
func noDelay(t *testing.T) {
	t.Helper()
	saved := deadlockBackoff
	deadlockBackoff.Base, deadlockBackoff.Max = 0, 0
	t.Cleanup(func() { deadlockBackoff = saved })
}

func TestWithinTranRetriesDeadlockVictim(t *testing.T) {
	noDelay(t)
	db := openTranDB(t)

	calls := 0
	err := WithinTran(context.Background(), nil, db, func(*sqlx.Tx) error {
		calls++
		if calls < 3 {
			return deadlockErr()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("want nil after retries, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("fn ran %d times, want 3", calls)
	}
}

func TestWithinTranGivesUpAfterBudget(t *testing.T) {
	noDelay(t)
	db := openTranDB(t)

	calls := 0
	err := WithinTran(context.Background(), nil, db, func(*sqlx.Tx) error {
		calls++
		return deadlockErr()
	})
	if !IsDeadlock(err) {
		t.Fatalf("want the deadlock error back, got %v", err)
	}
	if calls != deadlockBackoff.MaxAttempts {
		t.Fatalf("fn ran %d times, want %d", calls, deadlockBackoff.MaxAttempts)
	}
}

func TestWithinTranDoesNotRetryOtherErrors(t *testing.T) {
	noDelay(t)
	db := openTranDB(t)

	boom := errors.New("boom")
	calls := 0
	err := WithinTran(context.Background(), nil, db, func(*sqlx.Tx) error {
		calls++
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("want boom, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("fn ran %d times, want 1", calls)
	}
}

func TestIsDeadlock(t *testing.T) {
	if !IsDeadlock(deadlockErr()) {
		t.Fatal("wrapped 40P01 should be a deadlock")
	}
	if IsDeadlock(&pgconn.PgError{Code: uniqueViolation}) {
		t.Fatal("23505 is not a deadlock")
	}
	if IsDeadlock(nil) {
		t.Fatal("nil is not a deadlock")
	}
}
