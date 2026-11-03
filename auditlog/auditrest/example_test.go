package auditrest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/assanoff/skit/auditlog"
	"github.com/assanoff/skit/auditlog/auditrest"
	"github.com/assanoff/skit/auditlog/audittest"
	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/rest"
	"github.com/assanoff/skit/rest/router"
)

// Routes mounts the read side of the audit log on a router, behind an
// admin-only check here. The history lists the versions of the model, the
// changes the fields each version changed; a version that is not stored is
// 404, a version number that is missing 400.
func Example() {
	ctx := context.Background()
	core := auditlog.NewCore(nil, audittest.NewStore())
	for _, name := range []string{"gear", "big gear"} {
		core.Record(ctx, auditlog.NewAuditLog{
			ModelType: "widget", ModelID: "w-1", CreatedBy: "ann",
			Payload: map[string]string{"name": name},
		})
	}

	adminOnly := func(next rest.HandlerFunc) rest.HandlerFunc {
		return func(ctx context.Context, r *http.Request) rest.ResponseEncoder {
			if r.Header.Get("X-Role") != "admin" {
				return errs.Newf(errs.PermissionDenied, "admins only")
			}
			return next(ctx, r)
		}
	}
	r := router.New()
	auditrest.NewHandlers(core).Routes(r.HandleApp, adminOnly)

	get := func(path, role string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Role", role)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	w := get("/auditlog/widget/w-1", "admin")
	var hist []auditrest.AppAuditLog
	err := json.Unmarshal(w.Body.Bytes(), &hist)
	fmt.Println("history:", w.Code, err)
	for _, al := range hist {
		fmt.Println(al.Version, al.CreatedBy, string(al.Payload))
	}

	w = get("/auditlog/widget/w-1/changes", "admin")
	var recs []auditrest.AppRecord
	err = json.Unmarshal(w.Body.Bytes(), &recs)
	fmt.Println("changes:", w.Code, err)
	for _, rec := range recs {
		for _, f := range rec.ChangedFields {
			fmt.Printf("v%d %s: %v -> %v\n", rec.Version, f.Key, f.OldValue, f.NewValue)
		}
	}

	fmt.Println(get("/auditlog/widget/w-1/diff?current=1&target=9", "admin").Code)
	fmt.Println(get("/auditlog/widget/w-1/diff?current=1", "admin").Code)
	fmt.Println(get("/auditlog/widget/w-1", "").Code)
	// Output:
	// history: 200 <nil>
	// 1 ann {"name":"gear"}
	// 2 ann {"name":"big gear"}
	// changes: 200 <nil>
	// v1 name: <nil> -> gear
	// v2 name: gear -> big gear
	// 404
	// 400
	// 403
}
