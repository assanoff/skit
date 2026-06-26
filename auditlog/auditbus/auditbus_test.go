package auditbus_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/assanoff/skit/auditlog"
	"github.com/assanoff/skit/auditlog/auditbus"
	"github.com/assanoff/skit/auditlog/mocks"
	"github.com/assanoff/skit/eventbus"
)

func TestPublishRecordsViaBus(t *testing.T) {
	var saved []auditlog.AuditLog
	store := &mocks.StoreMock{
		QueryLastByModelIDFunc: func(context.Context, string, string) (auditlog.AuditLog, error) {
			return auditlog.AuditLog{}, auditlog.ErrNotFound
		},
		SaveFunc: func(_ context.Context, al auditlog.AuditLog) error {
			saved = append(saved, al)
			return nil
		},
	}
	core := auditlog.NewCore(nil, store)
	bus := eventbus.New(nil)
	auditbus.Register(bus, core)

	err := auditbus.Publish(context.Background(), bus, auditbus.Event{
		ModelType: "widget", ModelID: "1", Method: "POST", CreatedBy: "u1",
		Payload: json.RawMessage(`{"name":"a"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || saved[0].ModelID != "1" || saved[0].CreatedBy != "u1" {
		t.Fatalf("expected one record for model 1 by u1, got %+v", saved)
	}
}
