// Package auditbus records audit entries from events on the in-process
// eventbus, so a domain package asks for an audit write without importing
// auditlog: the producer publishes an Event on the bus, and the recorder that
// Register subscribes to it writes the entry.
//
// # Usage
//
// Subscribe the recorder once at startup, then publish an Event where the
// model changes:
//
//	auditbus.Register(bus, core) // *auditlog.Core or another auditlog.Recorder
//
//	err := auditbus.Publish(ctx, bus, auditbus.Event{
//	    ModelType: "widget",
//	    ModelID:   id,
//	    CreatedBy: actor,
//	    Payload:   snapshot, // the JSON of the model at this version
//	})
//
// The write is best-effort: the recorder handles a failed write itself, and
// Publish fails only on an Event that does not encode or decode. An entry that
// must commit with its change goes through auditlog.Core.Create over the
// transaction of the change.
//
// # Other buses
//
// Record writes one Event through the core and returns the error, so a
// consumer of a broker decodes a delivered message into an Event and decides
// the fate of the message by the error:
//
//	func(ctx context.Context, m broker.Message) broker.Action {
//	    var ev auditbus.Event
//	    if err := json.Unmarshal(m.Data, &ev); err != nil {
//	        return broker.Discard
//	    }
//	    if err := auditbus.Record(ctx, core, ev); err != nil {
//	        return broker.Requeue
//	    }
//	    return broker.Ack
//	}
//
// auditqueue uses Record the same way for the tasks of its queue.
package auditbus
