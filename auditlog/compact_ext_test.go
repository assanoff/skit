package auditlog_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/assanoff/skit/auditlog"
	"github.com/assanoff/skit/auditlog/mocks"
)

func TestCompactCallsStore(t *testing.T) {
	var deleted []int
	store := &mocks.StoreMock{
		VersionsFunc: func(context.Context, string, string) ([]int, error) {
			return []int{1, 2, 3, 4, 5}, nil
		},
		DeleteVersionsFunc: func(_ context.Context, _, _ string, versions []int) (int, error) {
			deleted = versions
			return len(versions), nil
		},
	}
	core := auditlog.NewCore(nil, store)

	n, err := core.Compact(context.Background(), "widget", "1", auditlog.CompactOptions{Factor: 2})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || !reflect.DeepEqual(deleted, []int{2, 4}) {
		t.Fatalf("deleted = %v (n=%d), want [2 4]", deleted, n)
	}
}

func TestAutoCompactOnWrite(t *testing.T) {
	var deleted []int
	store := &mocks.StoreMock{
		QueryLastByModelIDFunc: func(context.Context, string, string) (auditlog.AuditLog, error) {
			return auditlog.AuditLog{Version: 4, Payload: []byte(`{"name":"old"}`)}, nil
		},
		SaveFunc: func(context.Context, auditlog.AuditLog) error { return nil },
		VersionsFunc: func(context.Context, string, string) ([]int, error) {
			return []int{1, 2, 3, 4, 5}, nil
		},
		DeleteVersionsFunc: func(_ context.Context, _, _ string, versions []int) (int, error) {
			deleted = versions
			return len(versions), nil
		},
	}
	// every=1 → compaction runs after the write (which stores version 5).
	core := auditlog.NewCore(nil, store, auditlog.WithAutoCompact(1, auditlog.CompactOptions{Factor: 2}))

	if _, err := core.Create(context.Background(), auditlog.NewAuditLog{
		ModelType: "widget", ModelID: "1", Payload: map[string]any{"name": "new"},
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(deleted, []int{2, 4}) {
		t.Fatalf("auto-compact deleted = %v, want [2 4]", deleted)
	}
}

func TestCompactBatch(t *testing.T) {
	store := &mocks.StoreMock{
		OverThresholdFunc: func(context.Context, int, int) ([]auditlog.ModelRef, error) {
			return []auditlog.ModelRef{
				{ModelType: "widget", ModelID: "1", Versions: 5},
				{ModelType: "widget", ModelID: "2", Versions: 5},
			}, nil
		},
		VersionsFunc: func(context.Context, string, string) ([]int, error) {
			return []int{1, 2, 3, 4, 5}, nil
		},
		DeleteVersionsFunc: func(_ context.Context, _, _ string, versions []int) (int, error) {
			return len(versions), nil
		},
	}
	core := auditlog.NewCore(nil, store)

	res, err := core.CompactBatch(context.Background(), auditlog.CompactBatchOptions{
		Threshold: 3, Limit: 10, Compact: auditlog.CompactOptions{Factor: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Models != 2 || res.Deleted != 4 {
		t.Fatalf("result = %+v, want {Models:2 Deleted:4}", res)
	}
}
