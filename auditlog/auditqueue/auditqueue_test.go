package auditqueue_test

import (
	"context"
	"encoding/json/v2"
	"testing"

	"github.com/assanoff/skit/auditlog"
	"github.com/assanoff/skit/auditlog/auditbus"
	"github.com/assanoff/skit/auditlog/auditqueue"
	"github.com/assanoff/skit/queue"
)

type fakeQueue struct{ scheduled []queue.Params }

func (f *fakeQueue) Schedule(_ context.Context, p queue.Params) (bool, error) {
	f.scheduled = append(f.scheduled, p)
	return true, nil
}

func TestRecorderEnqueues(t *testing.T) {
	q := &fakeQueue{}
	r := auditqueue.NewRecorder(q, nil)

	r.Record(context.Background(), auditlog.NewAuditLog{
		ModelType: "widget", ModelID: "1", CreatedBy: "u1",
		Payload: map[string]any{"name": "a"},
	})

	if len(q.scheduled) != 1 {
		t.Fatalf("scheduled = %d, want 1", len(q.scheduled))
	}
	if q.scheduled[0].Queue != auditqueue.Queue || q.scheduled[0].Kind != auditqueue.Kind {
		t.Fatalf("queue, kind = %q, %q", q.scheduled[0].Queue, q.scheduled[0].Kind)
	}
	var ev auditbus.Event
	if err := json.Unmarshal(q.scheduled[0].Payload, &ev); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if ev.ModelID != "1" || ev.CreatedBy != "u1" || string(ev.Payload) != `{"name":"a"}` {
		t.Fatalf("event mismatch: %+v", ev)
	}
}
