package dbx

import (
	"context"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"

	"github.com/assanoff/skit/logger"
)

// maxParameters bounds how many bind parameters go into a single statement.
// Postgres caps this at 65535; we stay well under to leave headroom and to keep
// statements a reasonable size.
const maxParameters = 1000

// BulkInsert inserts len(values)/len(columns) rows into table in batches, each
// batch a single multi-row INSERT. conflictAction, if non-empty, is appended
// verbatim (e.g. "ON CONFLICT DO NOTHING"). values are laid out row-major:
// [r0c0, r0c1, r1c0, r1c1, ...].
//
// For upserts prefer BulkUpsert, which builds the conflict clause for you.
func BulkInsert(ctx context.Context, log *logger.Logger, db sqlx.ExtContext, table string, columns []string, values []any, conflictAction string) error {
	if len(columns) == 0 {
		return fmt.Errorf("dbx: bulk insert into %s: no columns", table)
	}
	if rem := len(values) % len(columns); rem != 0 {
		return fmt.Errorf("dbx: bulk insert into %s: len(values)=%d not a multiple of len(columns)=%d", table, len(values), len(columns))
	}
	if len(values) == 0 {
		return nil
	}

	// stride: how many values per batch, rounded down to a whole number of rows.
	stride := (maxParameters / len(columns)) * len(columns)
	if stride == 0 {
		return fmt.Errorf("dbx: bulk insert into %s: too many columns (%d)", table, len(columns))
	}

	for left := 0; left < len(values); left += stride {
		right := min(left+stride, len(values))
		batch := values[left:right]
		query := buildInsertQuery(table, columns, len(batch), conflictAction)
		if log != nil {
			log.Debug(ctx, "dbx.bulk_insert", "table", table, "rows", len(batch)/len(columns))
		}
		if _, err := db.ExecContext(ctx, query, batch...); err != nil {
			return fmt.Errorf("dbx: bulk insert into %s [%d:%d]: %w", table, left, right, translateError(err))
		}
	}
	return nil
}

// BulkUpsert inserts rows and, on conflict over conflictColumns, updates every
// non-conflict column from the proposed row.
func BulkUpsert(ctx context.Context, log *logger.Logger, db sqlx.ExtContext, table string, columns []string, values []any, conflictColumns []string) error {
	return BulkInsert(ctx, log, db, table, columns, values, buildUpsertConflictAction(columns, conflictColumns))
}

func buildInsertQuery(table string, columns []string, nValues int, conflictAction string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "INSERT INTO %s (%s) VALUES ", table, strings.Join(columns, ", "))

	nCols := len(columns)
	nRows := nValues / nCols
	param := 0
	for row := range nRows {
		if row > 0 {
			b.WriteString(", ")
		}
		b.WriteString("(")
		for col := range nCols {
			if col > 0 {
				b.WriteString(", ")
			}
			param++
			fmt.Fprintf(&b, "$%d", param)
		}
		b.WriteString(")")
	}

	if conflictAction != "" {
		b.WriteString(" ")
		b.WriteString(conflictAction)
	}
	return b.String()
}

func buildUpsertConflictAction(columns, conflictColumns []string) string {
	conflict := make(map[string]struct{}, len(conflictColumns))
	for _, c := range conflictColumns {
		conflict[c] = struct{}{}
	}

	var sets []string
	for _, c := range columns {
		if _, isKey := conflict[c]; isKey {
			continue
		}
		sets = append(sets, fmt.Sprintf("%s = excluded.%s", c, c))
	}
	if len(sets) == 0 {
		return fmt.Sprintf("ON CONFLICT (%s) DO NOTHING", strings.Join(conflictColumns, ", "))
	}
	return fmt.Sprintf("ON CONFLICT (%s) DO UPDATE SET %s", strings.Join(conflictColumns, ", "), strings.Join(sets, ", "))
}
