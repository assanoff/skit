// Package order describes the ordering of a list query: a By value (an
// allowlisted field name + an ASC/DESC direction) and a Parse that builds one
// from untrusted "field,direction" request input.
//
// Ordering is split across layers the same way pagination is. order owns the
// transport-agnostic value and its validation; the store owns the SQL — it maps
// the allowlisted field to a column and builds the ORDER BY clause, so the engine
// detail never leaks into the handler. Parse never copies raw input into
// By.Field: it only stores values drawn from the caller's allowlist, so the field
// is safe for the store to look up.
//
// # Usage
//
//	// Domain layer: the allowlist of sortable fields and the default order.
//	var sortable = map[string]string{"created_at": "created_at", "name": "name"}
//	var def = order.NewBy("created_at", order.DESC)
//
//	// Handler: parse untrusted ?order_by=, mapping a bad field/direction to 400.
//	by, err := order.Parse(sortable, r.URL.Query().Get("order_by"), def)
//	if err != nil {
//		return errs.New(errs.InvalidArgument, err)
//	}
//
//	// Store: map the allowlisted field to a column and build the clause.
//	col := columns[by.Field] // a store-local field -> column map
//	clause := " ORDER BY " + col + " " + by.Direction + ", id " + by.Direction
//
// Pair it with page (offset) or query for paginated listings. Cursor (keyset)
// pagination fixes its own order to match the cursor key, so order.By applies to
// offset queries.
package order
