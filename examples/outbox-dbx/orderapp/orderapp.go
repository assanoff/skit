package orderapp

import (
	"context"
	"errors"
)

// ErrNotFound reports an order that does not exist.
var ErrNotFound = errors.New("order not found")

// Status is the state of an order.
type Status string

// The states of an order.
const (
	StatusNew  Status = "new"
	StatusPaid Status = "paid"
)

// Order is an order of the shop.
type Order struct {
	ID     string
	Total  int64
	Status Status
}

// Payment is a payment for an order.
type Payment struct {
	Amount int64
}

// OrderCreated is the event of a new order. It imports nothing: its queue,
// the kind of its task and its topic are set by the assembly.
type OrderCreated struct {
	ID    string `json:"id"`
	Total int64  `json:"total"`
}

// Core is the business logic of the orders.
type Core struct {
	store Store // the only dependency: neither a pool, nor queue, nor a registry
}

// New builds the business logic over store.
func New(store Store) *Core {
	return &Core{store: store}
}

// Create writes a new order and schedules its event in one transaction.
func (c *Core) Create(ctx context.Context, o Order) error {
	o.Status = StatusNew
	return c.store.WithinTranTasks(ctx, func(r Repo, s Scheduler) error {
		if err := r.Create(ctx, o); err != nil {
			return err // rolls back the order and the event
		}
		return s.Schedule(ctx, OrderCreated{ID: o.ID, Total: o.Total})
	})
}

// Pay records the payment and marks the order paid in one transaction; it
// needs no event.
func (c *Core) Pay(ctx context.Context, id string, p Payment) error {
	return c.store.WithinTran(ctx, func(r Repo) error {
		if err := r.AddPayment(ctx, id, p); err != nil {
			return err
		}
		return r.SetStatus(ctx, id, StatusPaid)
	})
}

// Get reads an order without a transaction.
func (c *Core) Get(ctx context.Context, id string) (Order, error) {
	return c.store.QueryByID(ctx, id)
}
