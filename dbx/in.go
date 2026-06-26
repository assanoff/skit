package dbx

import (
	"context"

	"github.com/jmoiron/sqlx"

	"github.com/assanoff/skit/logger"
)

// NamedQuerySliceUsingIn runs a named query whose data contains a slice bound to
// an IN clause, scanning all rows into *[]T. It expands the slice parameters
// (sqlx.In) and rebinds the query for the driver before executing. Use it
// instead of NamedQuerySlice when the query has an `IN (:ids)` form.
func NamedQuerySliceUsingIn[T any](ctx context.Context, log *logger.Logger, db sqlx.ExtContext, query string, data any, dest *[]T) error {
	if log != nil {
		log.Debug(ctx, "dbx.query", "query", queryString(query, data))
	}

	rows, err := namedInQuery(ctx, db, query, data)
	if err != nil {
		return translateError(err)
	}
	defer func() { _ = rows.Close() }()

	var out []T
	for rows.Next() {
		var v T
		if err := rows.StructScan(&v); err != nil {
			return err
		}
		out = append(out, v)
	}
	*dest = out
	return rows.Err()
}

// NamedQueryStructUsingIn runs a named query with an IN clause expected to return
// exactly one row, scanning it into dest. Returns ErrDBNotFound when no row
// matches.
func NamedQueryStructUsingIn(ctx context.Context, log *logger.Logger, db sqlx.ExtContext, query string, data, dest any) error {
	if log != nil {
		log.Debug(ctx, "dbx.query", "query", queryString(query, data))
	}

	rows, err := namedInQuery(ctx, db, query, data)
	if err != nil {
		return translateError(err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return ErrDBNotFound
	}
	return rows.StructScan(dest)
}

// namedInQuery turns a named query plus data into a positional query with its
// slice parameters expanded (sqlx.Named -> sqlx.In) and rebound for the driver,
// then executes it.
func namedInQuery(ctx context.Context, db sqlx.ExtContext, query string, data any) (*sqlx.Rows, error) {
	named, args, err := sqlx.Named(query, data)
	if err != nil {
		return nil, err
	}
	expanded, args, err := sqlx.In(named, args...)
	if err != nil {
		return nil, err
	}
	return db.QueryxContext(ctx, db.Rebind(expanded), args...)
}
