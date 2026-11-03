package auditpg_test

import (
	"context"
	"log/slog"

	"github.com/assanoff/skit/auditlog"
	"github.com/assanoff/skit/auditlog/auditpg"
	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/dbx/pg"
)

// The store over the pool serves the queries of the audit log; a store over
// the transaction of a change commits its audit entry with it. The example
// needs Postgres, so go test builds it but does not run it.
func Example() {
	ctx := context.Background()
	log := slog.Default()
	db, err := pg.Open(pg.Config{Host: "localhost:5432", Name: "shop", DisableTLS: true})
	if err != nil {
		log.ErrorContext(ctx, "open", "err", err)
		return
	}
	defer func() { _ = db.Close() }()

	store := auditpg.NewStore(log, db)
	if err := store.EnsureSchema(ctx); err != nil {
		log.ErrorContext(ctx, "schema", "err", err)
		return
	}
	core := auditlog.NewCore(log, store)

	err = dbx.WithinTran(ctx, log, db, func(tx *dbx.Tx) error {
		const upd = `UPDATE widgets SET name = :name WHERE id = :id`
		w := map[string]any{"id": "w-1", "name": "big gear"}
		if err := dbx.NamedExec(ctx, tx, upd, w); err != nil {
			return err
		}
		_, err := auditlog.NewCore(log, auditpg.NewStore(log, tx)).Create(ctx,
			auditlog.NewAuditLog{
				ModelType: "widget", ModelID: "w-1", CreatedBy: "ann", Payload: w,
			})
		return err
	})
	if err != nil {
		log.ErrorContext(ctx, "rename widget", "err", err)
		return
	}

	hist, err := core.QueryHistoryByModelID(ctx, "widget", "w-1")
	if err != nil {
		log.ErrorContext(ctx, "history", "err", err)
		return
	}
	log.InfoContext(ctx, "widget history", "versions", len(hist))
}
