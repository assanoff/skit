package audittest_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/assanoff/skit/auditlog"
	"github.com/assanoff/skit/auditlog/audittest"
	"github.com/assanoff/skit/dbx"
)

// The in-memory Store backs an audit Core with the semantics of the Postgres
// store: a model that is not stored is auditlog.ErrNotFound, and a second
// write of one version is a duplicate.
func Example() {
	ctx := context.Background()
	store := audittest.NewStore()
	core := auditlog.NewCore(nil, store)

	_, err := core.QueryLastByModelID(ctx, "widget", "w-1")
	fmt.Println(errors.Is(err, auditlog.ErrNotFound))

	core.Record(ctx, auditlog.NewAuditLog{
		ModelType: "widget", ModelID: "w-1", CreatedBy: "ann",
		Payload: map[string]string{"name": "gear"},
	})
	last, err := core.QueryLastByModelID(ctx, "widget", "w-1")
	fmt.Println(last.ID, last.Version, last.CreatedBy, err)

	err = store.Save(ctx, last)
	fmt.Println(errors.Is(err, dbx.ErrDBDuplicatedEntry))
	// Output:
	// true
	// 1 1 ann <nil>
	// true
}
