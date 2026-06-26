package auditqueue_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/assanoff/skit/auditlog"
	"github.com/assanoff/skit/auditlog/auditbus"
	"github.com/assanoff/skit/auditlog/auditqueue"
	"github.com/assanoff/skit/queue"
)

type fakeQueue struct{ scheduled []queue.ScheduleParams }

func (f *fakeQueue) Schedule(_ context.Context, p queue.ScheduleParams) (bool, error) {
	f.scheduled = append(f.scheduled, p)
	return true, nil
}
func (f *fakeQueue) Claim(context.Context, time.Time, int) ([]queue.Task, error) { return nil, nil }
func (f *fakeQueue) MarkDone(context.Context, queue.Task, time.Time) error       { return nil }
func (f *fakeQueue) MarkFailed(context.Context, queue.Task, string, bool, time.Time) error {
	return nil
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
	if q.scheduled[0].Kind != auditqueue.Kind {
		t.Fatalf("kind = %q, want %q", q.scheduled[0].Kind, auditqueue.Kind)
	}
	var ev auditbus.Event
	if err := json.Unmarshal(q.scheduled[0].Payload, &ev); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if ev.ModelID != "1" || ev.CreatedBy != "u1" || string(ev.Payload) != `{"name":"a"}` {
		t.Fatalf("event mismatch: %+v", ev)
	}
}
