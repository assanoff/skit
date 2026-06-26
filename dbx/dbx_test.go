package dbx

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestStatusCheckSurfacesRealError verifies StatusCheck does not mask the real
// connection failure behind a bare context deadline. It points at a refused
// port (no Docker needed) and asserts the returned error carries both the
// deadline and the underlying "failed to connect" cause.
func TestStatusCheckSurfacesRealError(t *testing.T) {
	db, err := Open(Config{
		User: "u", Password: "p", Host: "127.0.0.1:1", Name: "x",
		DisableTLS: true, MaxOpenConns: 1, MaxIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err = StatusCheck(ctx, db)
	if err == nil {
		t.Fatal("expected an error against a refused port")
	}

	// The deadline is still detectable for callers that check for it.
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error should still wrap DeadlineExceeded, got: %v", err)
	}
	// ...but the real cause must be visible, not masked.
	if !strings.Contains(err.Error(), "connect") {
		t.Fatalf("error should surface the connection cause, got: %v", err)
	}
}
