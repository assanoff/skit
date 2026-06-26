package page

import "testing"

func TestNewClamps(t *testing.T) {
	tests := []struct {
		name         string
		number, rows int
		wantN, wantR int
		wantOffset   int
	}{
		{"defaults from zero", 0, 0, 1, DefaultRowsPerPage, 0},
		{"negative number -> 1", -3, 20, 1, 20, 0},
		{"over max rows -> cap", 2, MaxRowsPerPage + 100, 2, MaxRowsPerPage, MaxRowsPerPage},
		{"normal", 3, 25, 3, 25, 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(tt.number, tt.rows)
			if p.Number() != tt.wantN || p.RowsPerPage() != tt.wantR {
				t.Fatalf("New(%d,%d) = (%d,%d), want (%d,%d)", tt.number, tt.rows, p.Number(), p.RowsPerPage(), tt.wantN, tt.wantR)
			}
			if p.Offset() != tt.wantOffset {
				t.Fatalf("Offset = %d, want %d", p.Offset(), tt.wantOffset)
			}
		})
	}
}

func TestParseDefaultsAndValid(t *testing.T) {
	p, err := Parse("", "")
	if err != nil {
		t.Fatalf("Parse empty: %v", err)
	}
	if p.Number() != DefaultPageNumber || p.RowsPerPage() != DefaultRowsPerPage {
		t.Fatalf("empty defaults = %v", p)
	}

	p, err = Parse("2", "30")
	if err != nil || p.Number() != 2 || p.RowsPerPage() != 30 {
		t.Fatalf("Parse(2,30) = (%v, %v)", p, err)
	}
}

func TestParseRejectsOutOfRange(t *testing.T) {
	cases := []struct {
		name       string
		page, rows string
	}{
		{"non-numeric page", "abc", "10"},
		{"non-numeric rows", "1", "ten"},
		{"page too small", "0", "10"},
		{"negative page", "-1", "10"},
		{"rows too small", "1", "0"},
		{"rows too large", "1", "1000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Parse(c.page, c.rows); err == nil {
				t.Fatalf("Parse(%q,%q) must error", c.page, c.rows)
			}
		})
	}
}
