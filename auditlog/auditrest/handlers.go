package auditrest

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/assanoff/skit/auditlog"
	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/rest"
)

// Handlers is the read-side REST API for the audit log: history, diff between two
// versions, and changed-fields across all versions. Mount it on any skit
// router in one call with Routes; pair it with Middleware for the write side.
type Handlers struct {
	core *auditlog.Core
}

// NewHandlers builds the audit-log query handlers over core.
func NewHandlers(core *auditlog.Core) *Handlers {
	return &Handlers{core: core}
}

// Routes registers the audit-log read endpoints through the handle seam, so the
// audit API does not depend on the router type. Pass authorization middleware
// (e.g. admin-only) in mw to protect every endpoint:
//
//	auditrest.NewHandlers(auditCore).Routes(r.HandleApp, adminOnly)
func (h *Handlers) Routes(handle rest.Handle, mw ...rest.MidFunc) {
	handle("GET /auditlog/{model_type}/{model_id}", h.history, mw...)
	handle("GET /auditlog/{model_type}/{model_id}/diff", h.diff, mw...)
	handle("GET /auditlog/{model_type}/{model_id}/changes", h.changes, mw...)
}

// history returns every stored version of a model in ascending order.
func (h *Handlers) history(ctx context.Context, r *http.Request) rest.ResponseEncoder {
	mt, mid := r.PathValue("model_type"), r.PathValue("model_id")
	hist, err := h.core.QueryHistoryByModelID(ctx, mt, mid)
	if err != nil {
		return toErr(err)
	}
	return rest.JSON(toAppAuditLogs(hist))
}

// diff returns a textual diff between two versions selected via the required
// `current` and `target` query params. `base64=true` returns the diff encoded.
func (h *Handlers) diff(ctx context.Context, r *http.Request) rest.ResponseEncoder {
	mt, mid := r.PathValue("model_type"), r.PathValue("model_id")

	cur, err := queryInt(r, "current")
	if err != nil {
		return errs.Newf(errs.InvalidArgument, "invalid current version: %s", err)
	}
	target, err := queryInt(r, "target")
	if err != nil {
		return errs.Newf(errs.InvalidArgument, "invalid target version: %s", err)
	}

	var filter auditlog.QueryFilter
	filter.WithCurrentVersion(cur)
	filter.WithTargetVersion(target)

	d, err := h.core.QueryDiffByModelID(ctx, mt, mid, filter)
	if err != nil {
		return toErr(err)
	}
	d = h.core.NormalizeDiff(d)
	if r.URL.Query().Get("base64") == "true" {
		d = base64.StdEncoding.EncodeToString([]byte(d))
	}
	return rest.JSON(AppDiff{ModelType: mt, ModelID: mid, Diff: d})
}

// changes returns each version with the fields that changed since the previous.
func (h *Handlers) changes(ctx context.Context, r *http.Request) rest.ResponseEncoder {
	mt, mid := r.PathValue("model_type"), r.PathValue("model_id")
	recs, err := h.core.QueryDiffAllVersionByModelID(ctx, mt, mid, auditlog.QueryFilter{})
	if err != nil {
		return toErr(err)
	}
	return rest.JSON(toAppRecords(recs))
}

func queryInt(r *http.Request, key string) (int, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return 0, fmt.Errorf("missing %q", key)
	}
	return strconv.Atoi(v)
}

// toErr maps auditlog sentinel errors to the right HTTP status via *errs.Error.
func toErr(err error) *errs.Error {
	switch {
	case errors.Is(err, auditlog.ErrNotFound):
		return errs.New(errs.NotFound, err)
	case errors.Is(err, auditlog.ErrInvalidFilter):
		return errs.New(errs.InvalidArgument, err)
	default:
		return errs.New(errs.Internal, err)
	}
}
