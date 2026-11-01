package pgschema

import (
	"testing"

	"github.com/matryer/is"
)

// The keys of the SDK schemas stay what they were when dbx derived them:
// replicas of the previous version lock on the same keys.
func TestKeyStable(t *testing.T) {
	is := is.New(t)
	for name, want := range map[string]int64{
		"skit.queue.schema":       8781429109453582876,
		"skit.auditlog.schema":    -6116865666207823364,
		"skit.translation.schema": 6732127426380082672,
	} {
		is.Equal(Key(name), want) // the key of a name never changes
	}
}
