package dbx_test

import (
	"context"
	"database/sql"
	"testing"
	"uuid"

	"github.com/matryer/is"

	"github.com/assanoff/skit/dbtest"
	"github.com/assanoff/skit/dbx"
)

func TestUUIDValue(t *testing.T) {
	is := is.New(t)
	id := uuid.MustParse("0b0c3a9e-4f1d-4c39-9d54-5f0d5a8c7e21")

	v, err := dbx.UUID(id).Value()
	is.NoErr(err)
	is.Equal(v, "0b0c3a9e-4f1d-4c39-9d54-5f0d5a8c7e21") // binds as canonical text
}

func TestUUIDScan(t *testing.T) {
	id := uuid.MustParse("0b0c3a9e-4f1d-4c39-9d54-5f0d5a8c7e21")
	tests := []struct {
		name    string
		src     any
		wantErr bool
	}{
		{name: "string", src: id.String()},
		{name: "text bytes", src: []byte(id.String())},
		{name: "raw 16 bytes", src: id[:]},
		{name: "null", src: nil, wantErr: true},
		{name: "malformed", src: "not-a-uuid", wantErr: true},
		{name: "unsupported type", src: 42, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)
			var got dbx.UUID
			err := got.Scan(tt.src)
			if tt.wantErr {
				is.True(err != nil) // scan must fail
				return
			}
			is.NoErr(err)
			is.Equal(uuid.UUID(got), id) // scanned value must match
		})
	}
}

// TestUUIDRoundTripPostgres binds dbx.UUID through pgx in database/sql mode and
// scans it back, including a NULL through sql.Null[dbx.UUID].
func TestUUIDRoundTripPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires docker")
	}

	is := is.New(t)
	ctx := context.Background()
	db := dbtest.NewPostgres(ctx, t, dbtest.Config{}).DB

	_, err := db.ExecContext(ctx, `CREATE TABLE uuids (id UUID PRIMARY KEY, ref UUID)`)
	is.NoErr(err)

	type row struct {
		ID  dbx.UUID           `db:"id"`
		Ref sql.Null[dbx.UUID] `db:"ref"`
	}
	withRef := row{ID: dbx.UUID(uuid.New()), Ref: sql.Null[dbx.UUID]{V: dbx.UUID(uuid.New()), Valid: true}}
	noRef := row{ID: dbx.UUID(uuid.New())}

	const ins = `INSERT INTO uuids (id, ref) VALUES (:id, :ref)`
	for _, r := range []row{withRef, noRef} {
		_, err := db.NamedExecContext(ctx, ins, r)
		is.NoErr(err) // dbx.UUID must bind as a UUID parameter
	}

	var got row
	is.NoErr(db.GetContext(ctx, &got, `SELECT id, ref FROM uuids WHERE id = $1`, withRef.ID))
	is.Equal(got, withRef) // non-NULL ref round-trips

	got = row{}
	is.NoErr(db.GetContext(ctx, &got, `SELECT id, ref FROM uuids WHERE id = $1`, noRef.ID))
	is.Equal(got, noRef) // NULL ref scans as invalid sql.Null
}
