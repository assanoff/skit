package dbx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/assanoff/skit/retry"
)

// retryBackoff paces the re-runs of a transaction that failed with an error
// the Classifier marks ErrDBRetryable: on Postgres a deadlock victim (SQLSTATE
// 40P01) or a serialization failure (40001). Postgres resolves a deadlock after
// deadlock_timeout (1s by default), so by the first retry the winning
// transaction has almost always committed; the budget stays small because the
// caller blocks for the whole duration.
var retryBackoff = retry.Backoff{
	Base:        100 * time.Millisecond,
	Max:         time.Second,
	Factor:      2,
	MaxAttempts: 3,
	Jitter:      0.5,
}

// errTxDone is returned by Rollback after a successful Commit; it is benign.
var errTxDone = sql.ErrTxDone

// WithinTran runs fn so that its statements commit or roll back together.
//
// On a *DB it opens a transaction, commits it when fn returns nil and rolls it
// back on an error or a panic. An error the Classifier of the pool marks
// ErrDBRetryable rolls the transaction back in full, so WithinTran re-runs fn
// in a new one a few times (see retryBackoff) before returning the error: fn
// may run more than once, keep its side effects inside the transaction and no
// state between runs. log gets the retries and a failed rollback.
//
// On a *Tx it joins: fn runs in that transaction, and nothing is committed,
// rolled back or retried here — the WithinTran that opened it does that. A
// joined call is not a savepoint: return its error.
func WithinTran(ctx context.Context, log *slog.Logger, q Querier, fn func(tx *Tx) error) error {
	return q.within(ctx, log, fn)
}

func (d *DB) within(ctx context.Context, log *slog.Logger, fn func(tx *Tx) error) error {
	cfg := retry.Config{
		Backoff:    retryBackoff,
		IsTerminal: d.terminal,
	}
	if log != nil {
		cfg.OnRetry = func(attempt int, err error) {
			log.WarnContext(ctx, "dbx.tran.retry", "attempt", attempt, "err", err)
		}
	}
	return retry.Do(ctx, cfg, func(ctx context.Context) error {
		return d.tran(ctx, log, fn)
	})
}

// terminal reports whether a failed transaction must not run again: only an
// error the Classifier marks ErrDBRetryable may.
func (d *DB) terminal(err error) bool {
	return !errors.Is(d.classify(err), ErrDBRetryable)
}

func (d *DB) tran(ctx context.Context, log *slog.Logger, fn func(tx *Tx) error) error {
	if log != nil {
		log.DebugContext(ctx, "dbx.tran.begin")
	}
	tx, err := d.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("dbx: begin tran: %w", d.classify(err))
	}

	committed := false
	defer func() {
		if committed {
			return
		}
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, errTxDone) {
			if log != nil {
				log.ErrorContext(ctx, "dbx.tran.rollback failed", "err", rbErr)
			}
		}
	}()

	if err := fn(&Tx{tx: tx, cls: d.cls}); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("dbx: commit tran: %w", d.classify(err))
	}
	committed = true
	return nil
}

func (t *Tx) within(_ context.Context, _ *slog.Logger, fn func(tx *Tx) error) error {
	return fn(t)
}

// Tran runs a function in one transaction over U, a set of repositories,
// publishers and Cores of other domains bound to that transaction. The domain
// declares
//
//	type Tran interface {
//	    WithinTran(ctx context.Context, fn func(u Unit) error) error
//	}
//
// and the wiring passes NewTran(log, db, bind), which satisfies it without an
// adapter. A Tran over a *Tx joins that transaction, so Trans nest.
type Tran[U any] struct {
	log  *slog.Logger
	q    Querier
	bind func(tx *Tx) U
}

// NewTran returns a Tran over q, a pool or a transaction. bind builds U from
// the transaction and nothing else: it runs on every attempt, so a re-run gets
// a fresh U over the new transaction.
func NewTran[U any](log *slog.Logger, q Querier, bind func(tx *Tx) U) *Tran[U] {
	return &Tran[U]{log: log, q: q, bind: bind}
}

// WithinTran runs fn on bind(tx) as the package WithinTran does: over a *DB it
// opens the transaction, over a *Tx it joins it. fn may run more than once.
func (t *Tran[U]) WithinTran(ctx context.Context, fn func(u U) error) error {
	return WithinTran(ctx, t.log, t.q, func(tx *Tx) error {
		return fn(t.bind(tx))
	})
}
