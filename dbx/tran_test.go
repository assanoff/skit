package dbx

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"slices"
	"testing"
)

// tranDriver is a database/sql driver whose connections only know how to begin,
// commit and roll back — enough to drive WithinTran without a database.
type tranDriver struct{}

func (tranDriver) Open(string) (driver.Conn, error) { return tranConn{}, nil }

type tranConn struct{}

func (tranConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not implemented")
}
func (tranConn) Close() error              { return nil }
func (tranConn) Begin() (driver.Tx, error) { return tranTx{}, nil }

type tranTx struct{}

func (tranTx) Commit() error   { return nil }
func (tranTx) Rollback() error { return nil }

// tranDriverName registers tranDriver once and returns its name.
func tranDriverName() string {
	const name = "dbxtrandriver"
	if !slices.Contains(sql.Drivers(), name) {
		sql.Register(name, tranDriver{})
	}
	return name
}

// errDeadlock stands for a driver error that stubClassifier marks retryable.
var errDeadlock = errors.New("deadlock detected")

// stubClassifier marks errDeadlock retryable, as dbx/pg does 40P01.
type stubClassifier struct{}

func (stubClassifier) Classify(err error) error {
	if errors.Is(err, errDeadlock) {
		return fmt.Errorf("%w: %w", ErrDBRetryable, err)
	}
	return err
}

func openTranDB(t *testing.T) *DB {
	t.Helper()
	raw, err := sql.Open(tranDriverName(), "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { raw.Close() })
	return New(raw, "pgx", stubClassifier{})
}

func deadlockErr() error {
	return fmt.Errorf("namedexec: %w", errDeadlock)
}

// noDelay makes the retries instant for the duration of the test.
func noDelay(t *testing.T) {
	t.Helper()
	saved := retryBackoff
	retryBackoff.Base, retryBackoff.Max = 0, 0
	t.Cleanup(func() { retryBackoff = saved })
}

func TestWithinTranRetriesDeadlockVictim(t *testing.T) {
	noDelay(t)
	db := openTranDB(t)

	calls := 0
	err := WithinTran(context.Background(), nil, db, func(*Tx) error {
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

func TestWithinTranRetriesClassifiedError(t *testing.T) {
	noDelay(t)
	db := openTranDB(t)

	calls := 0
	err := WithinTran(context.Background(), nil, db, func(*Tx) error {
		calls++
		if calls < 2 {
			return fmt.Errorf("namedexec: %w: %w", ErrDBRetryable, errors.New("serialization failure"))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("want nil after a retry, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("fn ran %d times, want 2", calls)
	}
}

func TestWithinTranGivesUpAfterBudget(t *testing.T) {
	noDelay(t)
	db := openTranDB(t)

	calls := 0
	err := WithinTran(context.Background(), nil, db, func(*Tx) error {
		calls++
		return deadlockErr()
	})
	if !errors.Is(err, errDeadlock) {
		t.Fatalf("want the deadlock error back, got %v", err)
	}
	if calls != retryBackoff.MaxAttempts {
		t.Fatalf("fn ran %d times, want %d", calls, retryBackoff.MaxAttempts)
	}
}

func TestWithinTranDoesNotRetryOtherErrors(t *testing.T) {
	noDelay(t)
	db := openTranDB(t)

	boom := errors.New("boom")
	calls := 0
	err := WithinTran(context.Background(), nil, db, func(*Tx) error {
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
