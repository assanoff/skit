package orderdb

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/examples/outbox-dbx/orderapp"
	"github.com/assanoff/skit/queue"
)

// repo is the CRUD over the pool or a transaction.
type repo struct {
	log *slog.Logger
	q   dbx.Querier // *dbx.DB or *dbx.Tx
}

// withTx returns a copy of r bound to tx; the other fields carry over.
func (r *repo) withTx(tx *dbx.Tx) *repo {
	c := *r
	c.q = tx
	return &c
}

// Option configures a Store.
type Option func(*Store)

// WithTasks turns the tasks on: WithinTranTasks hands fn the scheduler that
// tasks returns for the transaction.
func WithTasks(tasks func(tx *dbx.Tx) *queue.Scheduler) Option {
	return func(s *Store) { s.tasks = tasks }
}

// Store over the pool opens transactions; over a transaction it joins it.
type Store struct {
	*repo // the CRUD on q

	tasks func(tx *dbx.Tx) *queue.Scheduler
}

var _ orderapp.Store = (*Store)(nil)

// NewStore builds a store over q: the pool (*dbx.DB) or a transaction
// (*dbx.Tx).
func NewStore(log *slog.Logger, q dbx.Querier, opts ...Option) *Store {
	s := &Store{repo: &repo{log: log, q: q}}
	for _, o := range opts {
		o(s)
	}
	return s
}

// EnsureSchema creates the tables orders and payments if absent.
func (s *Store) EnsureSchema(ctx context.Context) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS orders (
    id     TEXT PRIMARY KEY,
    total  BIGINT NOT NULL,
    status TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS payments (
    id       BIGSERIAL PRIMARY KEY,
    order_id TEXT NOT NULL REFERENCES orders (id),
    amount   BIGINT NOT NULL
);`
	if err := dbx.Exec(ctx, s.q, ddl); err != nil {
		return fmt.Errorf("orderdb: schema: %w", err)
	}
	return nil
}

// WithinTran runs fn in a transaction with the repo bound to it.
func (s *Store) WithinTran(ctx context.Context, fn func(r orderapp.Repo) error) error {
	return dbx.WithinTran(ctx, s.log, s.q, func(tx *dbx.Tx) error {
		return fn(s.withTx(tx))
	})
}

// WithinTranTasks runs fn in a transaction with the repo and the scheduler
// bound to it.
func (s *Store) WithinTranTasks(ctx context.Context, fn func(r orderapp.Repo, ts orderapp.Scheduler) error) error {
	return dbx.WithinTran(ctx, s.log, s.q, func(tx *dbx.Tx) error {
		return fn(s.withTx(tx), s.scheduler(tx))
	})
}

// scheduler returns the scheduler of tx, or noTasks without WithTasks.
func (s *Store) scheduler(tx *dbx.Tx) orderapp.Scheduler {
	if s.tasks == nil {
		return noTasks{}
	}
	return s.tasks(tx)
}

// noTasks is the scheduler of a store without tasks: Schedule fails, so a
// task is never dropped in silence.
type noTasks struct{}

func (noTasks) Schedule(context.Context, any) error {
	return errors.New("orderdb: the store has no tasks (WithTasks)")
}

// Create inserts the order.
func (r *repo) Create(ctx context.Context, o orderapp.Order) error {
	const q = `INSERT INTO orders (id, total, status) VALUES (:id, :total, :status)`
	if err := dbx.NamedExec(ctx, r.q, q, toDBOrder(o)); err != nil {
		return fmt.Errorf("orderdb: create: %w", err)
	}
	return nil
}

// AddPayment inserts a payment for the order id.
func (r *repo) AddPayment(ctx context.Context, id string, p orderapp.Payment) error {
	const q = `INSERT INTO payments (order_id, amount) VALUES (:order_id, :amount)`
	if err := dbx.NamedExec(ctx, r.q, q, paymentDB{OrderID: id, Amount: p.Amount}); err != nil {
		return fmt.Errorf("orderdb: add payment: %w", err)
	}
	return nil
}

// SetStatus sets the status of the order id.
func (r *repo) SetStatus(ctx context.Context, id string, st orderapp.Status) error {
	const q = `UPDATE orders SET status = :status WHERE id = :id`
	data := struct {
		ID     string `db:"id"`
		Status string `db:"status"`
	}{ID: id, Status: string(st)}
	n, err := dbx.NamedExecRowsAffected(ctx, r.q, q, data)
	if err != nil {
		return fmt.Errorf("orderdb: set status: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("orderdb: set status %s: %w", id, orderapp.ErrNotFound)
	}
	return nil
}

// QueryByID reads the order id; the dbx sentinel becomes the one of the
// domain.
func (r *repo) QueryByID(ctx context.Context, id string) (orderapp.Order, error) {
	const q = `SELECT id, total, status FROM orders WHERE id = :id`
	data := struct {
		ID string `db:"id"`
	}{ID: id}
	row, err := dbx.NamedQueryOne[orderDB](ctx, r.q, q, data)
	if errors.Is(err, dbx.ErrDBNotFound) {
		return orderapp.Order{}, fmt.Errorf("orderdb: query %s: %w", id, orderapp.ErrNotFound)
	}
	if err != nil {
		return orderapp.Order{}, fmt.Errorf("orderdb: query %s: %w", id, err)
	}
	return toCoreOrder(row), nil
}
