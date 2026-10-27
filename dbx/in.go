package dbx

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jmoiron/sqlx"
)

// NamedQuerySliceUsingIn runs a named query whose data contains a slice bound to
// an IN clause, scanning all rows into *[]T. It expands the slice parameters
// (sqlx.In) and rebinds the query for the driver before executing. Use it
// instead of NamedQuerySlice when the query has an `IN (:ids)` form.
func NamedQuerySliceUsingIn[T any](ctx context.Context, log *slog.Logger, db sqlx.ExtContext, query string, data any, dest *[]T) error {
	err := namedQuerySliceUsingIn(ctx, db, query, data, dest)
	return logQuery(ctx, log, caller(), "dbx.query", query, data, err)
}

func namedQuerySliceUsingIn[T any](ctx context.Context, db sqlx.ExtContext, query string, data any, dest *[]T) error {
	rows, err := namedInQuery(ctx, db, query, data)
	if err != nil {
		return translateError(err)
	}
	return scanAll(rows, dest)
}

// NamedQueryStructUsingIn runs a named query with an IN clause and scans its
// first row into dest. Returns ErrDBNotFound when no row matches.
func NamedQueryStructUsingIn(ctx context.Context, log *slog.Logger, db sqlx.ExtContext, query string, data, dest any) error {
	err := namedQueryStructUsingIn(ctx, db, query, data, dest)
	return logQuery(ctx, log, caller(), "dbx.query", query, data, err)
}

func namedQueryStructUsingIn(ctx context.Context, db sqlx.ExtContext, query string, data, dest any) error {
	rows, err := namedInQuery(ctx, db, query, data)
	if err != nil {
		return translateError(err)
	}
	return scanFirst(rows, dest)
}

// namedInQuery turns a named query plus data into a positional query with its
// slice parameters expanded (sqlx.Named -> sqlx.In) and rebound for the driver,
// then executes it.
func namedInQuery(ctx context.Context, db sqlx.ExtContext, query string, data any) (*sqlx.Rows, error) {
	named, args, err := sqlx.Named(query, data)
	if err != nil {
		return nil, fmt.Errorf("dbx: bind named: %w", err)
	}
	expanded, args, err := sqlx.In(named, args...)
	if err != nil {
		return nil, fmt.Errorf("dbx: expand in: %w", err)
	}
	rows, err := db.QueryxContext(ctx, db.Rebind(expanded), args...)
	if err != nil {
		return nil, fmt.Errorf("dbx: query: %w", err)
	}
	return rows, nil
}
