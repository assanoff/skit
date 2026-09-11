package dbx

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"

	"github.com/assanoff/skit/logger"
	"github.com/assanoff/skit/retry"
)

// deadlockBackoff paces the re-runs of a transaction that Postgres aborted as a
// deadlock victim (SQLSTATE 40P01). Postgres resolves a deadlock after
// deadlock_timeout (1s by default), so by the first retry the winning
// transaction has almost always committed; the budget stays small because the
// caller blocks for the whole duration.
var deadlockBackoff = retry.Backoff{
	Base:        100 * time.Millisecond,
	Max:         time.Second,
	Factor:      2,
	MaxAttempts: 3,
	Jitter:      0.5,
}

// Beginner starts a transaction. It is the seam stores depend on so they can be
// driven either by a pool or by an outer transaction (see middleware that opens
// a transaction per request).
type Beginner interface {
	Begin() (CommitRollbacker, error)
}

// CommitRollbacker is a transaction that can be committed or rolled back.
type CommitRollbacker interface {
	Commit() error
	Rollback() error
}

// DBBeginner adapts a *sqlx.DB to the Beginner interface.
type DBBeginner struct {
	db *sqlx.DB
}

// NewBeginner returns a Beginner backed by db.
func NewBeginner(db *sqlx.DB) *DBBeginner { return &DBBeginner{db: db} }

// Begin starts a new transaction.
func (b *DBBeginner) Begin() (CommitRollbacker, error) { return b.db.Beginx() }

// ExtContext extracts the sqlx.ExtContext (the query surface) from a transaction
// returned by Begin.
func ExtContext(tx CommitRollbacker) (sqlx.ExtContext, error) {
	ec, ok := tx.(sqlx.ExtContext)
	if !ok {
		return nil, errors.New("dbx: transaction does not implement sqlx.ExtContext")
	}
	return ec, nil
}

// WithinTran runs fn inside a transaction, committing on success and rolling
// back on error or panic.
//
// A transaction that Postgres aborts as a deadlock victim (SQLSTATE 40P01) is
// rolled back server-side in full, so WithinTran re-runs fn from scratch in a
// fresh transaction a few times (see deadlockBackoff) before returning the
// error. fn must therefore be safe to run more than once: keep every side
// effect inside the transaction and do not mutate captured state that the next
// run depends on. Any other error returns at once.
func WithinTran(ctx context.Context, log *logger.Logger, db *sqlx.DB, fn func(tx *sqlx.Tx) error) error {
	cfg := retry.Config{
		Backoff:    deadlockBackoff,
		IsTerminal: func(err error) bool { return !IsDeadlock(err) },
	}
	if log != nil {
		cfg.OnRetry = func(attempt int, err error) {
			log.Warn(ctx, "dbx.tran.deadlock, retrying", "attempt", attempt, "err", err)
		}
	}
	return retry.Do(ctx, cfg, func(ctx context.Context) error {
		return withinTran(ctx, log, db, fn)
	})
}

// IsDeadlock reports whether err wraps a Postgres deadlock (SQLSTATE 40P01).
// WithinTran retries such errors on its own; callers running statements
// outside a transaction can use it to decide on their own retry.
func IsDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == deadlockDetected
}

func withinTran(ctx context.Context, log *logger.Logger, db *sqlx.DB, fn func(tx *sqlx.Tx) error) error {
	if log != nil {
		log.Debug(ctx, "dbx.tran.begin")
	}
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("dbx: begin tran: %w", err)
	}

	committed := false
	defer func() {
		if committed {
			return
		}
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, errTxDone) {
			if log != nil {
				log.Error(ctx, "dbx.tran.rollback failed", "err", rbErr)
			}
		}
	}()

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("dbx: commit tran: %w", err)
	}
	committed = true
	return nil
}
