package page

import (
	"encoding/base64"
	"fmt"
)

// Cursor is a keyset (cursor) paging request: an opaque token marking the last
// item of the previous page plus a row limit. Cursor paging is stable under
// concurrent inserts and avoids large OFFSETs, at the cost of random access.
//
// The SDK owns the opaque token (encode/decode); the keyset predicate itself —
// e.g. WHERE (created_at, id) < (:after_ts, :after_id) — is engine- and
// entity-specific and stays in the store, which encodes the last row's key into
// the next cursor via EncodeCursor.
type Cursor struct {
	token string
	limit int
}

// NewCursor builds a Cursor, clamping the limit into [1, MaxRowsPerPage]
// (DefaultRowsPerPage when not positive). token is the opaque value from a prior
// page's next/prev link (empty for the first page).
func NewCursor(token string, limit int) Cursor {
	switch {
	case limit <= 0:
		limit = DefaultRowsPerPage
	case limit > MaxRowsPerPage:
		limit = MaxRowsPerPage
	}
	return Cursor{token: token, limit: limit}
}

// Token is the opaque cursor token (empty for the first page).
func (c Cursor) Token() string {
	return c.token
}

// Limit is the maximum number of rows to return.
func (c Cursor) Limit() int {
	return c.limit
}

// Key decodes the cursor token into the keyset value the store packed via
// EncodeCursor (e.g. "<created_at>|<id>"). An empty token yields "" (first page).
func (c Cursor) Key() (string, error) {
	if c.token == "" {
		return "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.token)
	if err != nil {
		return "", fmt.Errorf("page: invalid cursor: %w", err)
	}
	return string(raw), nil
}

// EncodeCursor packs a store's keyset value (the sort key of the boundary row)
// into an opaque, URL-safe cursor token. An empty key yields an empty token.
func EncodeCursor(key string) string {
	if key == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(key))
}
