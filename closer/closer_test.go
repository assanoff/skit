package closer

import (
	"errors"
	"testing"
)

func TestCloseSyncRunsLIFO(t *testing.T) {
	cl := New()
	var order []int
	cl.AddNamed("first", func() error { order = append(order, 1); return nil })
	cl.AddNamed("second", func() error { order = append(order, 2); return nil })
	cl.AddNamed("third", func() error { order = append(order, 3); return nil })

	if err := cl.CloseSync(); err != nil {
		t.Fatalf("CloseSync: %v", err)
	}
	want := []int{3, 2, 1}
	for i, v := range want {
		if order[i] != v {
			t.Fatalf("LIFO order = %v, want %v", order, want)
		}
	}
}

func TestCloseIsIdempotentAndJoinsErrors(t *testing.T) {
	cl := New()
	boom := errors.New("boom")
	calls := 0
	cl.Add(func() error { calls++; return boom })

	if err := cl.CloseSync(); !errors.Is(err, boom) {
		t.Fatalf("expected boom, got %v", err)
	}
	// Second close is a no-op (sync.Once) and must not re-run handlers.
	if err := cl.CloseSync(); err != nil {
		t.Fatalf("second close should be nil, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("handler ran %d times, want 1", calls)
	}
}

func TestAddAfterCloseIsIgnored(t *testing.T) {
	cl := New()
	_ = cl.CloseSync()
	ran := false
	cl.Add(func() error { ran = true; return nil })
	_ = cl.CloseSync()
	if ran {
		t.Fatal("handler added after close should not run")
	}
}
