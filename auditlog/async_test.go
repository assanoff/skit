package auditlog_test

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/matryer/is"

	"github.com/assanoff/skit/auditlog"
	"github.com/assanoff/skit/auditlog/mocks"
)

// newCountingCore returns a Core whose store records every Save into saved
// (guarded by mu). Every model id is treated as new (version 1), so each Record
// results in one Save.
func newCountingCore(saved *[]auditlog.AuditLog, mu *sync.Mutex) *auditlog.Core {
	store := &mocks.StoreMock{
		QueryLastByModelIDFunc: func(_ context.Context, _, _ string) (auditlog.AuditLog, error) {
			return auditlog.AuditLog{}, auditlog.ErrNotFound
		},
		SaveFunc: func(_ context.Context, al auditlog.AuditLog) error {
			mu.Lock()
			*saved = append(*saved, al)
			mu.Unlock()
			return nil
		},
	}
	return auditlog.NewCore(nil, store)
}

// TestAsyncRecorderDrainsAllOnShutdown verifies that entries buffered before
// shutdown are all written during the graceful drain (none lost).
func TestAsyncRecorderDrainsAllOnShutdown(t *testing.T) {
	is := is.New(t)

	var (
		mu    sync.Mutex
		saved []auditlog.AuditLog
	)
	core := newCountingCore(&saved, &mu)

	rec := auditlog.NewAsyncRecorder(core, auditlog.AsyncConfig{
		Workers: 2, Buffer: 200, BlockOnFull: true,
	})

	ctx, cancel := context.WithCancel(context.Background())
	startErr := make(chan error, 1)
	go func() { startErr <- rec.Start(ctx) }()

	const n = 100
	for i := range n {
		rec.Record(context.Background(), auditlog.NewAuditLog{
			ModelType: "widget", ModelID: strconv.Itoa(i), Method: "POST",
			Payload: map[string]any{"name": "a"},
		})
	}

	cancel()

	select {
	case err := <-startErr:
		is.NoErr(err)
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after ctx cancel")
	}

	mu.Lock()
	got := len(saved)
	mu.Unlock()
	is.Equal(got, n) // every buffered entry was drained, none lost
}

// TestAsyncRecorderDropsAfterShutdown verifies that Record after shutdown drops
// (calls OnDrop) instead of blocking or silently buffering an entry no worker
// will ever consume.
func TestAsyncRecorderDropsAfterShutdown(t *testing.T) {
	is := is.New(t)

	var (
		mu    sync.Mutex
		saved []auditlog.AuditLog
	)
	core := newCountingCore(&saved, &mu)

	var dropped int
	rec := auditlog.NewAsyncRecorder(core, auditlog.AsyncConfig{
		Workers: 1, Buffer: 8, BlockOnFull: true,
		OnDrop: func(auditlog.NewAuditLog) { dropped++ },
	})

	ctx, cancel := context.WithCancel(context.Background())
	startErr := make(chan error, 1)
	go func() { startErr <- rec.Start(ctx) }()

	cancel()
	select {
	case <-startErr:
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after ctx cancel")
	}

	// Post-shutdown Record must drop, even under BlockOnFull, rather than hang.
	done := make(chan struct{})
	go func() {
		rec.Record(context.Background(), auditlog.NewAuditLog{
			ModelType: "widget", ModelID: "late", Payload: map[string]any{"name": "a"},
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Record blocked after shutdown")
	}

	is.Equal(dropped, 1) // the late entry was dropped
}
