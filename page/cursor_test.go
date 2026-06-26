package page

import "testing"

func TestCursorRoundTrip(t *testing.T) {
	token := EncodeCursor("2026-06-25T00:00:00Z|42")
	if token == "" {
		t.Fatal("expected a non-empty token")
	}

	c := NewCursor(token, 0) // 0 limit -> default
	if c.Limit() != DefaultRowsPerPage {
		t.Fatalf("limit = %d, want default %d", c.Limit(), DefaultRowsPerPage)
	}
	key, err := c.Key()
	if err != nil {
		t.Fatalf("Key: %v", err)
	}
	if key != "2026-06-25T00:00:00Z|42" {
		t.Fatalf("decoded key = %q", key)
	}
}

func TestCursorFirstPageAndClamp(t *testing.T) {
	c := NewCursor("", MaxRowsPerPage+10)
	if c.Token() != "" {
		t.Fatalf("first-page token = %q, want empty", c.Token())
	}
	if c.Limit() != MaxRowsPerPage {
		t.Fatalf("limit = %d, want clamped %d", c.Limit(), MaxRowsPerPage)
	}
	key, err := c.Key()
	if err != nil || key != "" {
		t.Fatalf("first-page key = (%q, %v), want empty", key, err)
	}
}

func TestCursorInvalidToken(t *testing.T) {
	if _, err := NewCursor("not!base64!", 10).Key(); err == nil {
		t.Fatal("expected error decoding an invalid cursor token")
	}
}

func TestEncodeCursorEmpty(t *testing.T) {
	if EncodeCursor("") != "" {
		t.Fatal("empty key must encode to empty token")
	}
}
