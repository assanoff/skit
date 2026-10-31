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
			want:    "INSERT INTO t (id, name) VALUES (?, ?)",
		},
		{
			name:    "three rows two cols",
			columns: []string{"id", "name"},
			nValues: 6,
			want:    "INSERT INTO t (id, name) VALUES (?, ?), (?, ?), (?, ?)",
		},
		{
			name:           "with conflict action",
			columns:        []string{"id"},
			nValues:        2,
			conflictAction: "ON CONFLICT DO NOTHING",
			want:           "INSERT INTO t (id) VALUES (?), (?) ON CONFLICT DO NOTHING",
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
