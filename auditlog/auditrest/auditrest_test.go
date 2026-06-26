package auditrest_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/assanoff/skit/auditlog"
	"github.com/assanoff/skit/auditlog/auditrest"
	"github.com/assanoff/skit/auditlog/mocks"
	"github.com/assanoff/skit/rest/router"
)

func serve(t *testing.T, store *mocks.StoreMock) *httptest.Server {
	t.Helper()
	core := auditlog.NewCore(nil, store)
	r := router.New()
	auditrest.NewHandlers(core).Routes(r.HandleApp)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func TestHistoryEndpoint(t *testing.T) {
	store := &mocks.StoreMock{
		QueryHistoryByModelIDFunc: func(_ context.Context, modelType, modelID string) ([]auditlog.AuditLog, error) {
			return []auditlog.AuditLog{
				{ID: 1, Version: 1, ModelType: modelType, ModelID: modelID, Payload: []byte(`{"name":"a"}`), CreatedAt: time.Unix(0, 0)},
				{ID: 2, Version: 2, ModelType: modelType, ModelID: modelID, Payload: []byte(`{"name":"b"}`), CreatedAt: time.Unix(0, 0)},
			}, nil
		},
	}
	srv := serve(t, store)

	resp, err := http.Get(srv.URL + "/auditlog/widget/1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2 versions, got %d", len(out))
	}
}

func TestDiffEndpoint(t *testing.T) {
	store := &mocks.StoreMock{
		QueryModelByVersionFunc: func(_ context.Context, modelType, modelID string, ver int) (auditlog.AuditLog, error) {
			payload := `{"name":"a"}`
			if ver == 2 {
				payload = `{"name":"b"}`
			}
			return auditlog.AuditLog{Version: ver, ModelType: modelType, ModelID: modelID, Payload: []byte(payload)}, nil
		},
	}
	srv := serve(t, store)

	resp, err := http.Get(srv.URL + "/auditlog/widget/1/diff?current=1&target=2")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if d, _ := out["diff"].(string); d == "" {
		t.Fatalf("expected a non-empty diff, got %v", out)
	}
}

func TestDiffMissingVersionParam(t *testing.T) {
	srv := serve(t, &mocks.StoreMock{})
	resp, err := http.Get(srv.URL + "/auditlog/widget/1/diff?current=1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
