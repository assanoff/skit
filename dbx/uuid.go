package dbx

import (
	"database/sql/driver"
	"fmt"
	"uuid"
)

// UUID adapts the standard library uuid.UUID to a Postgres UUID column. The
// stdlib type is a bare [16]byte without sql.Scanner / driver.Valuer, and
// database/sql cannot scan into an array, so a uuid.UUID db-model field fails
// StructScan. A UUID field scans the driver's text or 16-byte form and binds as
// the canonical string, so the store keeps one row struct:
//
//	type dbAdvert struct {
//	    ID      dbx.UUID              `db:"id"`
//	    OwnerID sql.Null[dbx.UUID]    `db:"owner_id"` // nullable column
//	}
//
// Convert at the core/db boundary with plain conversions: dbx.UUID(a.ID) and
// uuid.UUID(row.ID). A SQL NULL is a scan error; use sql.Null[dbx.UUID] for a
// nullable column.
type UUID uuid.UUID

// Value implements driver.Valuer: bind the UUID as its canonical string.
func (u UUID) Value() (driver.Value, error) {
	return uuid.UUID(u).String(), nil
}

// Scan implements sql.Scanner: accept the textual form or the raw 16 bytes.
func (u *UUID) Scan(src any) error {
	switch v := src.(type) {
	case string:
		return u.parse(v)
	case []byte:
		if len(v) == len(u) {
			copy(u[:], v)
			return nil
		}
		return u.parse(string(v))
	case nil:
		return fmt.Errorf("dbx: cannot scan NULL into UUID (use sql.Null[dbx.UUID])")
	}
	return fmt.Errorf("dbx: cannot scan %T into UUID", src)
}

func (u *UUID) parse(s string) error {
	id, err := uuid.Parse(s)
	if err != nil {
		return fmt.Errorf("dbx: scan UUID: %w", err)
	}
	*u = UUID(id)
	return nil
}
