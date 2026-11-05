package auditpg_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/matryer/is"

	"github.com/assanoff/skit/auditlog"
	"github.com/assanoff/skit/auditlog/auditpg"
	"github.com/assanoff/skit/dbtest"
	"github.com/assanoff/skit/dbx"
)

// TestStore checks the auditlog.Store contract on Postgres: what audittest
// does in memory, the store does in the audit_log table.
func TestStore(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires docker")
	}

	is := is.New(t)
	ctx := context.Background()

	pg := dbtest.NewPostgres(ctx, t, dbtest.Config{})
	store := auditpg.NewStore(slog.New(slog.DiscardHandler), pg.DB)
	is.NoErr(store.EnsureSchema(ctx)) // the audit_log table must exist
	is.NoErr(store.EnsureSchema(ctx)) // a second run must be a no-op

	at := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	save := func(typ, id string, ver int, payload string) error {
		return store.Save(ctx, auditlog.AuditLog{
			ModelType: typ,
			ModelID:   id,
			Version:   ver,
			Method:    "PUT",
			Path:      "/widgets/" + id,
			Payload:   []byte(payload),
			CreatedAt: at,
			CreatedBy: "ann",
		})
	}
	for ver, p := range []int{10, 12, 15} {
		is.NoErr(save("widget", "w-1", ver+1, fmt.Sprintf(`{"price":%d}`, p)))
	}
	is.NoErr(save("widget", "w-2", 1, `{"price":7}`))
	is.NoErr(save("order", "o-1", 1, `{"total":3}`))

	t.Run("duplicate version", func(t *testing.T) {
		is := is.New(t)
		err := save("widget", "w-1", 2, `{"price":99}`)
		is.True(errors.Is(err, dbx.ErrDBDuplicatedEntry)) // so Core.Create retries
	})

	t.Run("last", func(t *testing.T) {
		is := is.New(t)
		last, err := store.QueryLastByModelID(ctx, "widget", "w-1")
		is.NoErr(err)
		is.True(last.ID > 0) // assigned by the database
		is.Equal(last.Version, 3)
		is.Equal(last.ModelType, "widget")
		is.Equal(last.ModelID, "w-1")
		is.Equal(last.Method, "PUT")
		is.Equal(last.Path, "/widgets/w-1")
		is.Equal(price(t, last.Payload), 15)
		is.True(last.CreatedAt.Equal(at))
		is.Equal(last.CreatedBy, "ann")

		_, err = store.QueryLastByModelID(ctx, "widget", "w-9")
		is.True(errors.Is(err, auditlog.ErrNotFound))
	})

	t.Run("history", func(t *testing.T) {
		is := is.New(t)
		hist, err := store.QueryHistoryByModelID(ctx, "widget", "w-1")
		is.NoErr(err)
		is.Equal(len(hist), 3)
		for i, al := range hist {
			is.Equal(al.Version, i+1) // ascending
		}

		hist, err = store.QueryHistoryByModelID(ctx, "widget", "w-9")
		is.NoErr(err)
		is.Equal(len(hist), 0)
	})

	t.Run("by version", func(t *testing.T) {
		is := is.New(t)
		al, err := store.QueryModelByVersion(ctx, "widget", "w-1", 2)
		is.NoErr(err)
		is.Equal(price(t, al.Payload), 12)

		_, err = store.QueryModelByVersion(ctx, "widget", "w-1", 9)
		is.True(errors.Is(err, auditlog.ErrNotFound))
	})

	t.Run("over threshold", func(t *testing.T) {
		is := is.New(t)
		refs, err := store.OverThreshold(ctx, 1, 10)
		is.NoErr(err)
		is.Equal(refs, []auditlog.ModelRef{{ModelType: "widget", ModelID: "w-1", Versions: 3}})

		refs, err = store.OverThreshold(ctx, 0, 1)
		is.NoErr(err)
		is.Equal(len(refs), 1) // the limit holds
		is.Equal(refs[0].ModelID, "w-1")
	})

	t.Run("delete versions", func(t *testing.T) {
		is := is.New(t)
		n, err := store.DeleteVersions(ctx, "widget", "w-2", nil)
		is.NoErr(err)
		is.Equal(n, 0)

		n, err = store.DeleteVersions(ctx, "widget", "w-1", []int{1, 3, 9})
		is.NoErr(err)
		is.Equal(n, 2) // version 9 does not exist
		vers, err := store.Versions(ctx, "widget", "w-1")
		is.NoErr(err)
		is.Equal(vers, []int{2})
	})
}

// price decodes the price of a widget payload; Postgres reformats JSONB, so
// the bytes are not compared.
func price(t *testing.T, payload []byte) int {
	t.Helper()
	var w struct {
		Price int `json:"price"`
	}
	if err := json.Unmarshal(payload, &w); err != nil {
		t.Fatalf("decode payload %s: %v", payload, err)
	}
	return w.Price
}
