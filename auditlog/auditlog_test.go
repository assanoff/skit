package auditlog_test

import (
	"context"
	"testing"

	"github.com/assanoff/skit/auditlog"
	"github.com/assanoff/skit/auditlog/mocks"
)

func TestCreateFirstVersion(t *testing.T) {
	var saved []auditlog.AuditLog
	store := &mocks.StoreMock{
		QueryLastByModelIDFunc: func(ctx context.Context, modelType string, modelID string) (auditlog.AuditLog, error) {
			return auditlog.AuditLog{}, auditlog.ErrNotFound
		},
		SaveFunc: func(ctx context.Context, al auditlog.AuditLog) error {
			saved = append(saved, al)
			return nil
		},
	}
	core := auditlog.NewCore(nil, store)

	got, err := core.Create(context.Background(), auditlog.NewAuditLog{
		ModelType: "widget", ModelID: "1", Method: "POST", Path: "/widgets",
		Payload: map[string]any{"name": "a"}, CreatedBy: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 {
		t.Fatalf("version = %d, want 1", got.Version)
	}
	if len(saved) != 1 {
		t.Fatalf("save calls = %d, want 1", len(saved))
	}
	if saved[0].CreatedBy != "u1" || saved[0].ModelType != "widget" {
		t.Fatalf("saved entry mismatch: %+v", saved[0])
	}
}

func TestCreateSkipsUnchangedVersion(t *testing.T) {
	var saveCalls int
	store := &mocks.StoreMock{
		QueryLastByModelIDFunc: func(ctx context.Context, modelType string, modelID string) (auditlog.AuditLog, error) {
			return auditlog.AuditLog{Version: 1, Payload: []byte(`{"name":"a"}`)}, nil
		},
		SaveFunc: func(ctx context.Context, al auditlog.AuditLog) error {
			saveCalls++
			return nil
		},
	}
	core := auditlog.NewCore(nil, store)

	// Payload differs only in the normalized-away fields → treated as unchanged.
	got, err := core.Create(context.Background(), auditlog.NewAuditLog{
		ModelType: "widget", ModelID: "1",
		Payload: map[string]any{"name": "a", "updated_at": "2026-01-01T00:00:00Z", "updated_by": 7},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 0 {
		t.Fatalf("expected no new version, got %d", got.Version)
	}
	if saveCalls != 0 {
		t.Fatalf("save calls = %d, want 0 (unchanged)", saveCalls)
	}
}

func TestCreateBumpsVersionOnChange(t *testing.T) {
	var saved []auditlog.AuditLog
	store := &mocks.StoreMock{
		QueryLastByModelIDFunc: func(ctx context.Context, modelType string, modelID string) (auditlog.AuditLog, error) {
			return auditlog.AuditLog{Version: 1, Payload: []byte(`{"name":"a"}`)}, nil
		},
		SaveFunc: func(ctx context.Context, al auditlog.AuditLog) error {
			saved = append(saved, al)
			return nil
		},
	}
	core := auditlog.NewCore(nil, store)

	got, err := core.Create(context.Background(), auditlog.NewAuditLog{
		ModelType: "widget", ModelID: "1",
		Payload: map[string]any{"name": "b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 {
		t.Fatalf("version = %d, want 2", got.Version)
	}
	if len(saved) != 1 {
		t.Fatalf("save calls = %d, want 1", len(saved))
	}
}
