package orderdb

import "github.com/assanoff/skit/examples/outbox-dbx/orderapp"

// orderDB is a row of the table orders.
//
//exhaustruct:enforce
type orderDB struct {
	ID     string `db:"id"`
	Total  int64  `db:"total"`
	Status string `db:"status"`
}

func toDBOrder(o orderapp.Order) orderDB {
	return orderDB{ID: o.ID, Total: o.Total, Status: string(o.Status)}
}

func toCoreOrder(r orderDB) orderapp.Order {
	//exhaustruct:enforce
	return orderapp.Order{ID: r.ID, Total: r.Total, Status: orderapp.Status(r.Status)}
}

// paymentDB is a row of the table payments.
//
//exhaustruct:enforce
type paymentDB struct {
	OrderID string `db:"order_id"`
	Amount  int64  `db:"amount"`
}
