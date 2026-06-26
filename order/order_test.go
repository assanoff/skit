package order_test

import (
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/order"
)

var allowed = map[string]string{
	"created_at": "created_at",
	"name":       "name",
}

func TestParse(t *testing.T) {
	def := order.NewBy("created_at", order.DESC)

	tests := []struct {
		name      string
		in        string
		wantField string
		wantDir   string
		wantErr   bool
	}{
		{"empty -> default", "", "created_at", order.DESC, false},
		{"field only -> ASC", "name", "name", order.ASC, false},
		{"field + DESC", "name,DESC", "name", order.DESC, false},
		{"field + ASC", "created_at,ASC", "created_at", order.ASC, false},
		{"trims spaces", " name , DESC ", "name", order.DESC, false},
		{"unknown field", "bogus", "", "", true},
		{"unknown direction", "name,desc", "", "", true},
		{"too many parts", "name,DESC,extra", "", "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			is := is.New(t)

			by, err := order.Parse(allowed, tc.in, def)
			if tc.wantErr {
				is.True(err != nil) // rejected
				return
			}
			is.NoErr(err)                    // accepted
			is.Equal(by.Field, tc.wantField) // mapped field
			is.Equal(by.Direction, tc.wantDir)
		})
	}
}

// TestNewByDefaultsDirection verifies NewBy falls back to ASC for an unknown
// direction and keeps a known one.
func TestNewByDefaultsDirection(t *testing.T) {
	is := is.New(t)

	is.Equal(order.NewBy("name", "sideways").Direction, order.ASC)  // unknown -> ASC
	is.Equal(order.NewBy("name", order.DESC).Direction, order.DESC) // known kept
}
