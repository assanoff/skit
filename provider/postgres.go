package provider

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"

	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/dim"
)

// Postgres returns a dim factory that opens a pgx-backed connection pool from
// cfg and verifies connectivity (dbx.StatusCheck). The cleanup closes the
// pool. Wire it with dim.NewResource:
//
//	c.DB, cleanup = dim.NewResource("DB", provider.Postgres(dbx.Config{
//		User: opts.DB.User, Password: opts.DB.Password, Host: opts.DB.Host,
//		Name: opts.DB.Name, DisableTLS: opts.DB.DisableTLS,
//	}))
func Postgres(cfg dbx.Config) func(ctx context.Context) (*sqlx.DB, dim.CleanupFunc, error) {
	return func(ctx context.Context) (*sqlx.DB, dim.CleanupFunc, error) {
		db, err := dbx.Open(cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("provider: open postgres: %w", err)
		}
		if err := dbx.StatusCheck(ctx, db); err != nil {
			_ = db.Close()
			return nil, nil, fmt.Errorf("provider: postgres status check: %w", err)
		}
		return db, func() error { return db.Close() }, nil
	}
}
