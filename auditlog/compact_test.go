package auditlog

import (
	"reflect"
	"testing"
)

func TestVersionsToDelete(t *testing.T) {
	tests := []struct {
		name string
		vers []int
		opts CompactOptions
		want []int
	}{
		{"below min keeps all", []int{1, 2}, CompactOptions{Factor: 2}, nil},
		{"factor 2 keeps first/last/every-2nd", []int{1, 2, 3, 4, 5}, CompactOptions{Factor: 2}, []int{2, 4}},
		{"keep recent protects tail", []int{1, 2, 3, 4, 5}, CompactOptions{Factor: 2, KeepRecent: 2}, []int{2}},
		{"max versions caps total", []int{1, 2, 3, 4, 5, 6}, CompactOptions{MaxVersions: 3}, []int{3, 4, 5}},
		{"no factor no cap keeps all", []int{1, 2, 3, 4}, CompactOptions{}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := versionsToDelete(tt.vers, tt.opts)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("versionsToDelete(%v, %+v) = %v, want %v", tt.vers, tt.opts, got, tt.want)
			}
			for _, d := range got {
				if d == tt.vers[0] || d == tt.vers[len(tt.vers)-1] {
					t.Fatalf("must never delete first/last, deleted %d", d)
				}
			}
		})
	}
}
