package dbx

import "testing"

func TestAdvisoryKeyStableAndDistinct(t *testing.T) {
	// Stable: the same name yields the same key on every call.
	const name = "skit.outbox.schema"
	first := AdvisoryKey(name)
	for range 3 {
		if got := AdvisoryKey(name); got != first {
			t.Fatalf("AdvisoryKey(%q) not stable: %d != %d", name, got, first)
		}
	}

	// Distinct: the keys the SDK storage packages use must not collide.
	names := []string{
		"skit.outbox.schema",
		"skit.queue.schema",
		"skit.auditlog.schema",
		"skit.translation.schema",
	}
	seen := make(map[int64]string, len(names))
	for _, n := range names {
		k := AdvisoryKey(n)
		if prev, dup := seen[k]; dup {
			t.Fatalf("advisory key collision: %q and %q both map to %d", prev, n, k)
		}
		seen[k] = n
	}
}
