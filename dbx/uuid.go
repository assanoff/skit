package dbx

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"uuid"
)

// UUID is a uuid.UUID that binds on any driver. database/sql scans and binds
// the standard uuid.UUID itself, and pgx and modernc.org/sqlite take it as
// is; a driver that converts arguments on its own, such as
// go-sql-driver/mysql, rejects it. UUID binds as the canonical string through
// driver.Valuer (a CHAR(36) column on MySQL) and scans the driver's text or
// 16-byte form, so a store that must run on such a driver keeps one row
// struct:
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
		return errors.New("dbx: cannot scan NULL into UUID (use sql.Null[dbx.UUID])")
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
