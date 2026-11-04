package orderapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/assanoff/skit/broker"
)

// Order is an order of the shop.
type Order struct {
	ID    string `json:"id"`
	Total int64  `json:"total"`
}

// payment is the data of the event payment.paid of the billing service.
type payment struct {
	OrderID string `json:"order_id"`
}

// App is the domain of the orders.
type App struct {
	log *slog.Logger
	pub broker.Publisher
}

// New builds the domain over pub; the assembly decides what stands behind it.
func New(log *slog.Logger, pub broker.Publisher) *App {
	return &App{log: log, pub: pub}
}

// Create announces the order: the event order.created goes to the topic
// orders with the order as its JSON data.
func (a *App) Create(ctx context.Context, o Order) error {
	data, err := json.Marshal(o)
	if err != nil {
		return fmt.Errorf("orderapp: create: %w", err)
	}
	err = a.pub.Publish(ctx, broker.Message{
		Type:        "order.created",
		ContentType: "application/json",
		Data:        data,
		Topic:       "orders",
	})
	if err != nil {
		return fmt.Errorf("orderapp: create: %w", err)
	}
	return nil
}

// HandlePaid consumes the event payment.paid. A payment that names no order
// is discarded: delivered again, it would fail again.
func (a *App) HandlePaid(ctx context.Context, m broker.Message) broker.Action {
	var p payment
	if err := json.Unmarshal(m.Data, &p); err != nil || p.OrderID == "" {
		a.log.WarnContext(ctx, "orderapp: a payment without an order",
			"event_id", m.ID, "err", err)
		return broker.Discard
	}
	a.log.InfoContext(ctx, "order paid", "order_id", p.OrderID)
	return broker.Ack
}
