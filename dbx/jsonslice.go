package dbx

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// JSONSlice adapts a Go slice to a Postgres JSONB column. skit runs pgx v5 in
// database/sql mode (via sqlx), where database/sql cannot scan a Postgres array
// (text[], int8[]) back into a Go slice — there is no sql.Scanner for a bare
// slice, so an array-valued column fails StructScan. Storing the slice as JSONB
// and (un)marshaling it here sidesteps that: a JSONSlice[T] field StructScans
// cleanly and binds as a named parameter, so the store still uses one row struct.
//
// Use it as the db-model field type for any slice-valued column:
//
//	type dbAdvert struct {
//	    Photos dbx.JSONSlice[string] `db:"photos"`
//	}
//
// backed by a column declared `photos JSONB NOT NULL DEFAULT '[]'::jsonb`.
type JSONSlice[T any] []T

// Value implements driver.Valuer: marshal the slice to JSON for the JSONB column.
// A nil slice is written as an empty JSON array, not SQL NULL, so a NOT NULL
// column with a '[]' default round-trips.
func (s JSONSlice[T]) Value() (driver.Value, error) {
	if s == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]T(s))
}

// Scan implements sql.Scanner: unmarshal the JSONB payload back into the slice.
// A SQL NULL and an empty payload both yield a nil slice.
func (s *JSONSlice[T]) Scan(src any) error {
	if src == nil {
		*s = nil
		return nil
	}

	var b []byte
	switch v := src.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("dbx: cannot scan %T into JSONSlice", src)
	}

	if len(b) == 0 {
		*s = nil
		return nil
	}
	return json.Unmarshal(b, (*[]T)(s))
}
