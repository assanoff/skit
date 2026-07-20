package dbx

import (
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

// errTxDone is returned by Rollback after a successful Commit; it is benign.
var errTxDone = sql.ErrTxDone

// queryString renders a named query with its parameters inlined, for logging
// only. It is best-effort and never used to execute SQL, so the substitution is
// intentionally simple (no SQL-escaping concerns since it does not run). Values
// of secret-looking columns (see isSensitive) are redacted so credentials never
// reach a log line; call it only behind a log-enabled guard.
func queryString(query string, args any) string {
	q := strings.Join(strings.Fields(query), " ")

	v := reflect.ValueOf(args)
	for v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return q
	}

	type binding struct{ placeholder, value string }
	var bindings []binding
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("db")
		if tag == "" || tag == "-" {
			continue
		}
		value := "'***'"
		if !isSensitive(tag) {
			value = formatValue(v.Field(i).Interface())
		}
		bindings = append(bindings, binding{placeholder: ":" + tag, value: value})
	}

	// Replace longer placeholders first so ":id" does not corrupt ":id_owner".
	sort.Slice(bindings, func(i, j int) bool {
		return len(bindings[i].placeholder) > len(bindings[j].placeholder)
	})
	for _, b := range bindings {
		q = strings.ReplaceAll(q, b.placeholder, b.value)
	}
	return q
}

// isSensitive reports whether a db column name looks like it holds a secret, so
// queryString redacts its value instead of inlining it into a debug log line.
func isSensitive(tag string) bool {
	lower := strings.ToLower(tag)
	for _, s := range []string{
		"password", "passwd", "secret", "token",
		"api_key", "apikey", "private_key", "access_key", "credential",
	} {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
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
