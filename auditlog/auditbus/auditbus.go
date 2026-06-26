// Package auditbus records audit entries from events on the in-process eventbus,
// decoupling producers from the audit core: a producer publishes an Event on the
// bus and Register subscribes the audit core to consume it, so a domain package
// can request an audit write without importing auditlog.
//
// The same Record helper is exported for an external-bus (broker) consumer to
// reuse: decode a delivered message into an Event and call Record. Together with
// the auditrest middleware and a direct Core.Create call, the audit log can be
// fed four ways — internal bus, external bus, HTTP/gRPC middleware, or directly.
package auditbus

import (
	"context"
	"encoding/json"

	"github.com/assanoff/skit/auditlog"
	"github.com/assanoff/skit/eventbus"
)

// Domain and Action name the eventbus (domain, action) audit events use.
const (
	Domain = "auditlog"
	Action = "record"
)

// Event is the payload a producer publishes to record an audit entry. Payload is
// the raw JSON snapshot of the model at this version.
type Event struct {
	ModelType string          `json:"model_type"`
	ModelID   string          `json:"model_id"`
	Method    string          `json:"method"`
	Path      string          `json:"path"`
	CreatedBy string          `json:"created_by"`
	Payload   json.RawMessage `json:"payload"`
}

// Register subscribes rec to (Domain, Action) events on the bus. Wire it once at
// startup. rec may be a *auditlog.Core (synchronous) or an asynchronous recorder;
// recording is best-effort, so the bus handler decodes and forwards to rec.Record
// and only a decode error is returned to the bus.
func Register(bus *eventbus.Bus, rec auditlog.Recorder) {
	bus.Register(Domain, Action, func(ctx context.Context, d eventbus.Data) error {
		ev, err := eventbus.Decode[Event](d)
		if err != nil {
			return err
		}
		rec.Record(ctx, ev.toNewAuditLog())
		return nil
	})
}

// Publish emits an audit Event on the bus for the registered consumer
// (best-effort). Call bus.Call with the same data when the producer must abort on
// a failed audit write.
func Publish(ctx context.Context, bus *eventbus.Bus, ev Event) error {
	return bus.Publish(ctx, eventbus.MustData(Domain, Action, ev))
}

// Record writes a single audit Event through core synchronously, returning the
// error. Exported so an external-broker consumer can decode a delivery into an
// Event and reuse the in-process path with full error handling (ack/nack).
func Record(ctx context.Context, core *auditlog.Core, ev Event) error {
	_, err := core.Create(ctx, ev.toNewAuditLog())
	return err
}

func (ev Event) toNewAuditLog() auditlog.NewAuditLog {
	return auditlog.NewAuditLog{
		ModelType: ev.ModelType,
		ModelID:   ev.ModelID,
		Method:    ev.Method,
		Path:      ev.Path,
		Payload:   ev.Payload,
		CreatedBy: ev.CreatedBy,
	}
}
