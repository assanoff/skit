package auditrest

import (
	"encoding/json"
	"time"

	"github.com/assanoff/skit/auditlog"
)

// AppAuditLog is the REST representation of a single audit log version.
type AppAuditLog struct {
	ID        int             `json:"id"`
	Version   int             `json:"version"`
	ModelType string          `json:"model_type"`
	ModelID   string          `json:"model_id"`
	Method    string          `json:"method"`
	Path      string          `json:"path"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
	CreatedBy string          `json:"created_by"`
}

// AppDiff is the textual diff between two versions of a model.
type AppDiff struct {
	ModelType string `json:"model_type"`
	ModelID   string `json:"model_id"`
	Diff      string `json:"diff"`
}

// AppField is a single changed attribute between two versions.
type AppField struct {
	Key      string `json:"key"`
	OldValue any    `json:"old_value"`
	NewValue any    `json:"new_value"`
}

// AppRecord is one version with the fields that changed since the previous one.
type AppRecord struct {
	Version       int        `json:"version"`
	ModelType     string     `json:"model_type"`
	ModelID       string     `json:"model_id"`
	Method        string     `json:"method"`
	Path          string     `json:"path"`
	UpdatedBy     *string    `json:"updated_by,omitempty"`
	UpdatedAt     *time.Time `json:"updated_at,omitempty"`
	ChangedFields []AppField `json:"changed_fields"`
}

func toAppAuditLog(al auditlog.AuditLog) AppAuditLog {
	return AppAuditLog{
		ID:        al.ID,
		Version:   al.Version,
		ModelType: al.ModelType,
		ModelID:   al.ModelID,
		Method:    al.Method,
		Path:      al.Path,
		Payload:   json.RawMessage(al.Payload),
		CreatedAt: al.CreatedAt,
		CreatedBy: al.CreatedBy,
	}
}

func toAppAuditLogs(als []auditlog.AuditLog) []AppAuditLog {
	out := make([]AppAuditLog, len(als))
	for i, al := range als {
		out[i] = toAppAuditLog(al)
	}
	return out
}

func toAppRecord(r auditlog.DiffRecord) AppRecord {
	fields := make([]AppField, len(r.ChangedFields))
	for i, f := range r.ChangedFields {
		fields[i] = AppField{Key: f.Key, OldValue: f.OldValue, NewValue: f.NewValue}
	}
	return AppRecord{
		Version:       r.Version,
		ModelType:     r.ModelType,
		ModelID:       r.ModelID,
		Method:        r.Method,
		Path:          r.Path,
		UpdatedBy:     r.UpdatedBy,
		UpdatedAt:     r.UpdatedAt,
		ChangedFields: fields,
	}
}

func toAppRecords(recs []auditlog.DiffRecord) []AppRecord {
	out := make([]AppRecord, len(recs))
	for i, r := range recs {
		out[i] = toAppRecord(r)
	}
	return out
}
