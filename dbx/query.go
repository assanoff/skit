package dbx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"
)

// Exec runs a statement as written: it takes no parameters and is not parsed
// for :name, so DDL, casts (::) and colons in literals pass through.
func Exec(ctx context.Context, q sqlx.ExtContext, query string) error {
	_, err := exec(ctx, q, query)
	return err
}

// ExecRowsAffected is Exec that returns the number of rows the statement
// changed. The count means changed, not matched (MySQL without
// clientFoundRows counts an UPDATE to the same values as 0), so what 0 means
// is the caller's decision.
func ExecRowsAffected(ctx context.Context, q sqlx.ExtContext, query string) (int64, error) {
	res, err := exec(ctx, q, query)
	if err != nil {
		return 0, err
	}
	return rowsAffected(res)
}

// NamedExec runs a statement with :name parameters bound from arg, a struct
// with `db` tags or a map[string]any.
func NamedExec(ctx context.Context, q sqlx.ExtContext, query string, arg any) error {
	_, err := namedExec(ctx, q, query, arg)
	return err
}

// NamedExecRowsAffected is NamedExec that returns the number of rows the
// statement changed; see ExecRowsAffected. A store that updates or deletes by
// key turns 0 into its own not-found error.
func NamedExecRowsAffected(ctx context.Context, q sqlx.ExtContext, query string, arg any) (int64, error) {
	res, err := namedExec(ctx, q, query, arg)
	if err != nil {
		return 0, err
	}
	return rowsAffected(res)
}

// QueryOne runs a query as written (see Exec) and scans its first row into T,
// a struct with `db` tags or a scalar. No rows is ErrDBNotFound; an error later
// in the result is returned, not lost.
func QueryOne[T any](ctx context.Context, q sqlx.ExtContext, query string) (T, error) {
	var v T
	if err := sqlx.GetContext(ctx, q, &v, query); err != nil {
		return v, translateError(err)
	}
	return v, nil
}

// NamedQueryOne is QueryOne for a query with :name parameters bound from arg.
func NamedQueryOne[T any](ctx context.Context, q sqlx.ExtContext, query string, arg any) (T, error) {
	var v T
	bound, args, err := q.BindNamed(query, arg)
	if err != nil {
		return v, fmt.Errorf("dbx: bind named: %w", err)
	}
	if err := sqlx.GetContext(ctx, q, &v, bound, args...); err != nil {
		return v, translateError(err)
	}
	return v, nil
}

// QueryList runs a query as written (see Exec) and scans every row into a T, a
// struct with `db` tags or a scalar. No rows is a nil slice and no error.
func QueryList[T any](ctx context.Context, q sqlx.ExtContext, query string) ([]T, error) {
	return list[T](ctx, q, query)
}

// NamedQueryList is QueryList for a query with :name parameters bound from arg.
func NamedQueryList[T any](ctx context.Context, q sqlx.ExtContext, query string, arg any) ([]T, error) {
	bound, args, err := q.BindNamed(query, arg)
	if err != nil {
		return nil, fmt.Errorf("dbx: bind named: %w", err)
	}
	return list[T](ctx, q, bound, args...)
}

// NamedQueryListUsingIn is NamedQueryList for a query with an IN (:ids) clause:
// it expands the slice parameters (sqlx.In) and rebinds the query for the
// driver. It is the portable form of ANY, which works on Postgres only. An
// empty slice is an error: guard it before the call.
func NamedQueryListUsingIn[T any](ctx context.Context, q sqlx.ExtContext, query string, arg any) ([]T, error) {
	named, args, err := sqlx.Named(query, arg)
	if err != nil {
		return nil, fmt.Errorf("dbx: bind named: %w", err)
	}
	expanded, args, err := sqlx.In(named, args...)
	if err != nil {
		return nil, fmt.Errorf("dbx: expand in: %w", err)
	}
	return list[T](ctx, q, q.Rebind(expanded), args...)
}

func exec(ctx context.Context, q sqlx.ExtContext, query string) (sql.Result, error) {
	res, err := q.ExecContext(ctx, query)
	if err != nil {
		return nil, translateError(err)
	}
	return res, nil
}

func namedExec(ctx context.Context, q sqlx.ExtContext, query string, arg any) (sql.Result, error) {
	res, err := sqlx.NamedExecContext(ctx, q, query, arg)
	if err != nil {
		return nil, translateError(err)
	}
	return res, nil
}

func rowsAffected(res sql.Result) (int64, error) {
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("dbx: rows affected: %w", err)
	}
	return n, nil
}

// list scans every row of the query into a T; on an error it returns no rows.
func list[T any](ctx context.Context, q sqlx.ExtContext, query string, args ...any) ([]T, error) {
	var vs []T
	if err := sqlx.SelectContext(ctx, q, &vs, query, args...); err != nil {
		return nil, translateError(err)
	}
	return vs, nil
}

// translateError maps well-known Postgres errors onto the package sentinels.
// It wraps the driver error rather than replacing it, so the *pgconn.PgError
// with its constraint and detail stays reachable through errors.As.
func translateError(err error) error {
	if err == nil {
		return nil
	}
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		switch pgErr.Code {
		case uniqueViolation:
			return fmt.Errorf("%w: %w", ErrDBDuplicatedEntry, err)
		case undefinedTable:
			return fmt.Errorf("%w: %w", ErrUndefinedTable, err)
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDBNotFound
	}
	return err
}
