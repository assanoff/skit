package postgres

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"

	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/dim"
)

// Resource returns a dim factory that opens a connection pool from cfg
// (dbx.Open) and verifies it (dbx.StatusCheck). The cleanup closes the pool.
func Resource(cfg dbx.Config) func(ctx context.Context) (*sqlx.DB, dim.CleanupFunc, error) {
	return func(ctx context.Context) (*sqlx.DB, dim.CleanupFunc, error) {
		db, err := dbx.Open(cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("postgres: open: %w", err)
		}
		if err := dbx.StatusCheck(ctx, db); err != nil {
			_ = db.Close()
			return nil, nil, fmt.Errorf("postgres: status check: %w", err)
		}
		return db, db.Close, nil
	}
}
