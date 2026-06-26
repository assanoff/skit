package dbx

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// errTxDone is returned by Rollback after a successful Commit; it is benign.
var errTxDone = sql.ErrTxDone

// queryString renders a named query with its parameters inlined, for logging
// only. It is best-effort and never used to execute SQL, so the substitution is
// intentionally simple (no SQL-escaping concerns since it does not run).
func queryString(query string, args any) string {
	q := strings.Join(strings.Fields(query), " ")

	v := reflect.ValueOf(args)
	for v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return q
	}

	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("db")
		if tag == "" || tag == "-" {
			continue
		}
		placeholder := ":" + tag
		q = strings.ReplaceAll(q, placeholder, formatValue(v.Field(i).Interface()))
	}
	return q
}

func formatValue(val any) string {
	switch x := val.(type) {
	case nil:
		return "NULL"
	case string:
		return "'" + x + "'"
	case time.Time:
		return "'" + x.Format(time.RFC3339) + "'"
	default:
		return fmt.Sprintf("%v", x)
	}
}
