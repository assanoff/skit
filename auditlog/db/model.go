package db

import (
	"time"

	"github.com/assanoff/skit/auditlog"
)

// auditLogDB is the database representation of an auditlog.AuditLog row. Payload
// maps to a JSONB column as raw bytes.
type auditLogDB struct {
	ID        int       `db:"id"`
	ModelType string    `db:"model_type"`
	ModelID   string    `db:"model_id"`
	Version   int       `db:"version"`
	Method    string    `db:"method"`
	Path      string    `db:"path"`
	Payload   []byte    `db:"payload"`
	CreatedAt time.Time `db:"created_at"`
	CreatedBy string    `db:"created_by"`
}

func toDBAuditLog(al auditlog.AuditLog) auditLogDB {
	return auditLogDB{
		ModelType: al.ModelType,
		ModelID:   al.ModelID,
		Version:   al.Version,
		Method:    al.Method,
		Path:      al.Path,
		Payload:   al.Payload,
		CreatedAt: al.CreatedAt,
		CreatedBy: al.CreatedBy,
	}
}

func toCoreAuditLog(r auditLogDB) auditlog.AuditLog {
	return auditlog.AuditLog{
		ID:        r.ID,
		ModelID:   r.ModelID,
		ModelType: r.ModelType,
		Version:   r.Version,
		Method:    r.Method,
		Path:      r.Path,
		Payload:   r.Payload,
		CreatedAt: r.CreatedAt,
		CreatedBy: r.CreatedBy,
	}
}

func toCoreAuditLogSlice(rows []auditLogDB) []auditlog.AuditLog {
	out := make([]auditlog.AuditLog, len(rows))
	for i, r := range rows {
		out[i] = toCoreAuditLog(r)
	}
	return out
}
