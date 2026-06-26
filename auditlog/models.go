package auditlog

import "time"

// AuditLog is a single, versioned snapshot of a model recorded in the audit
// trail. Entries for the same (ModelType, ModelID) form an ordered history;
// Version increments by one per stored change. Payload is the JSON-encoded
// snapshot of the model at that version. CreatedBy is the actor (e.g. the JWT
// subject) that made the change.
type AuditLog struct {
	ID        int
	Version   int
	ModelType string
	ModelID   string
	Method    string
	Path      string
	Payload   []byte
	CreatedAt time.Time
	CreatedBy string
}

// NewAuditLog is the input to Core.Create. Payload is any value that JSON-encodes
// to the model snapshot; the version and CreatedAt are assigned by Create (last
// version + 1, or skipped when unchanged). Method/Path label the transport action
// (HTTP verb + path, or the gRPC full method).
type NewAuditLog struct {
	ModelID   string
	ModelType string
	Method    string
	Path      string
	Payload   any
	CreatedBy string
}
