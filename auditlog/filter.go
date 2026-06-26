package auditlog

import "github.com/assanoff/skit/errs"

// QueryFilter selects which versions a diff query compares. Both versions are
// required for QueryDiffByModelID.
type QueryFilter struct {
	CurrentVersion *int `validate:"omitempty,numeric,min=1"`
	TargetVersion  *int `validate:"omitempty,numeric,min=1"`
}

// Validate reports whether the filter is well-formed, returning an
// errs.InvalidArgument error with per-field detail when not.
func (qf *QueryFilter) Validate() error {
	return errs.Check(qf)
}

// WithCurrentVersion sets the current-version field.
func (qf *QueryFilter) WithCurrentVersion(ver int) { qf.CurrentVersion = &ver }

// WithTargetVersion sets the target-version field.
func (qf *QueryFilter) WithTargetVersion(ver int) { qf.TargetVersion = &ver }
