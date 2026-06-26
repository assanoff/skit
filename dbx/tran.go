package dbx

import (
	"context"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"

	"github.com/assanoff/skit/logger"
)

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
func WithinTran(ctx context.Context, log *logger.Logger, db *sqlx.DB, fn func(tx *sqlx.Tx) error) error {
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
