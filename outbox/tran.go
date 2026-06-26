package outbox

import (
	"context"

	"github.com/jmoiron/sqlx"

	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/logger"
)

// PublishFunc is the caller's transactional closure. It receives the
// transaction handle for domain writes and a tx-bound Publisher for emitting
// events. Returning an error rolls back everything — domain writes and the
// events recorded through pub together.
//
// The domain code inside fn never sees pub's mechanics: it calls
// pub.Publish(ctx, SomeEvent{...}) with a plain typed value. The tx handle is
// here so an application can bind its own store to the transaction (e.g.
// store.WithTx(tx)); domain methods themselves take the bound store, not tx.
type PublishFunc func(tx *sqlx.Tx, pub Publisher) error

// WithinTran runs fn inside one SQL transaction with a tx-bound Publisher.
// Events published through pub are inserted in the SAME transaction, so domain
// writes and their events commit atomically — the core guarantee of the
// transactional outbox: an event is persisted if and only if the domain change
// that produced it commits. Any error (from fn or an insert) rolls everything
// back.
//
// reg resolves each published value's route (topic/key/content type). This is a
// thin wrapper over dbx.WithinTran; the domain never threads tx through its
// event code.
func WithinTran(ctx context.Context, log *logger.Logger, db *sqlx.DB, store Store, reg *Registry, fn PublishFunc) error {
	return dbx.WithinTran(ctx, log, db, func(tx *sqlx.Tx) error {
		pub := Bind(tx, store, reg)
		return fn(tx, pub)
	})
}
