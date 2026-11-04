package orderapp

import "context"

// Repo is the CRUD of the orders. The callback of a transaction gets it bound
// to the transaction.
type Repo interface {
	Create(ctx context.Context, o Order) error
	AddPayment(ctx context.Context, id string, p Payment) error
	SetStatus(ctx context.Context, id string, st Status) error
	QueryByID(ctx context.Context, id string) (Order, error)
}

// Scheduler schedules a task — an event, any work after the commit — in the
// same transaction. The domain declares it itself and does not import queue.
type Scheduler interface {
	Schedule(ctx context.Context, v any) error
}

// Store is the CRUD and the transactions. fn may run again (a retry on a
// deadlock): inside it use only r and s, keep no state between the runs and
// do not call Store. A Store built over a transaction of the caller joins it
// and does not commit.
type Store interface {
	Repo

	// WithinTran runs fn in one transaction; r is bound to it.
	WithinTran(ctx context.Context, fn func(r Repo) error) error

	// WithinTranTasks is WithinTran whose s schedules tasks in the same
	// transaction: the writes and the tasks commit or roll back together.
	WithinTranTasks(ctx context.Context, fn func(r Repo, s Scheduler) error) error
}
