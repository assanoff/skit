package order

import (
	"fmt"
	"strings"
)

// Directions for data ordering.
const (
	ASC  = "ASC"
	DESC = "DESC"
)

var directions = map[string]string{
	ASC:  ASC,
	DESC: DESC,
}

// By is a field to order by plus a direction. Field is an allowlisted name (not
// raw user input): Parse only ever sets it from a caller-supplied mapping, so it
// is safe for a store to interpolate into a column lookup. Direction is ASC or
// DESC.
type By struct {
	Field     string
	Direction string
}

// NewBy builds a By, defaulting an unknown direction to ASC. It performs no field
// validation — pair it with a fixed field name (e.g. a default order), or use
// Parse for untrusted input.
func NewBy(field, direction string) By {
	if _, ok := directions[direction]; !ok {
		return By{Field: field, Direction: ASC}
	}
	return By{Field: field, Direction: direction}
}

// Parse builds a By from an untrusted "field[,direction]" string (e.g.
// "name,DESC"). allowed is the allowlist: it maps an accepted field name to the
// value stored in By.Field (often itself, or a domain key a store later maps to a
// column) — a field absent from allowed is rejected, so a client cannot order by
// an arbitrary column. An empty orderBy yields def. Direction defaults to ASC and
// must be ASC or DESC.
func Parse(allowed map[string]string, orderBy string, def By) (By, error) {
	if orderBy == "" {
		return def, nil
	}

	parts := strings.Split(orderBy, ",")

	field, ok := allowed[strings.TrimSpace(parts[0])]
	if !ok {
		return By{}, fmt.Errorf("order: unknown field %q", strings.TrimSpace(parts[0]))
	}

	switch len(parts) {
	case 1:
		return NewBy(field, ASC), nil

	case 2:
		direction := strings.TrimSpace(parts[1])
		if _, ok := directions[direction]; !ok {
			return By{}, fmt.Errorf("order: unknown direction %q", direction)
		}
		return NewBy(field, direction), nil

	default:
		return By{}, fmt.Errorf("order: malformed order %q", orderBy)
	}
}
