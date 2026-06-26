package dialect

import "bytes"

// Dialect describes the engine-specific behavior a store needs in order to
// compose portable SQL from shared fragments.
type Dialect interface {
	// Name reports a short identifier for the engine (for logging only).
	Name() string

	// Paginate appends a pagination clause to buf. The clause consumes the named
	// bind variables ":offset" and ":rows_per_page", which the caller must supply
	// in the parameter map.
	Paginate(buf *bytes.Buffer)
}
