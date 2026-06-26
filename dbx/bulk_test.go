package dbx

import "testing"

func TestBuildInsertQuery(t *testing.T) {
	tests := []struct {
		name           string
		columns        []string
		nValues        int
		conflictAction string
		want           string
	}{
		{
			name:    "single row two cols",
			columns: []string{"id", "name"},
			nValues: 2,
			want:    "INSERT INTO t (id, name) VALUES ($1, $2)",
		},
		{
			name:    "three rows two cols",
			columns: []string{"id", "name"},
			nValues: 6,
			want:    "INSERT INTO t (id, name) VALUES ($1, $2), ($3, $4), ($5, $6)",
		},
		{
			name:           "with conflict action",
			columns:        []string{"id"},
			nValues:        2,
			conflictAction: "ON CONFLICT DO NOTHING",
			want:           "INSERT INTO t (id) VALUES ($1), ($2) ON CONFLICT DO NOTHING",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildInsertQuery("t", tc.columns, tc.nValues, tc.conflictAction)
			if got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestBuildUpsertConflictAction(t *testing.T) {
	got := buildUpsertConflictAction([]string{"id", "name", "email"}, []string{"id"})
	want := "ON CONFLICT (id) DO UPDATE SET name = excluded.name, email = excluded.email"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}

	// All columns are conflict keys -> DO NOTHING.
	got = buildUpsertConflictAction([]string{"id"}, []string{"id"})
	want = "ON CONFLICT (id) DO NOTHING"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}
