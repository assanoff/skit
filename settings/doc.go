// Package settings keeps the settings that an admin changes while the
// service runs — limits, switches, texts — in Postgres, and their current
// value in the memory of every replica.
//
// Settings are one document of a type T under a name, stored as JSONB in the
// table settings. Every replica reads them at its start and rereads them on
// an interval; a hot path reads Current, which never waits for the database
// and never fails. An admin handler stores a new document with Save.
//
// # Usage
//
// The table comes from EnsureSchema at the start, or from Schema in the
// migrations of the service. New reads the settings, with the defaults for
// what is not stored; the group of the service runs the rereads:
//
//	type Limits struct {
//	    MaxWithdrawal int64 `json:"max_withdrawal"`
//	    Maintenance   bool  `json:"maintenance"`
//	}
//
//	if err := settings.EnsureSchema(ctx, log, db); err != nil {
//	    return err
//	}
//	limits, err := settings.New(ctx, log, db, "limits", Limits{
//	    MaxWithdrawal: 100_000,
//	})
//	if err != nil {
//	    return err
//	}
//	group.Add(limits) // rereads every 30s
//
// A hot path reads the settings from memory:
//
//	if limits.Current().Maintenance {
//	    return errs.New(errs.Unavailable, ErrMaintenance)
//	}
//
// An admin handler checks a new document and saves it:
//
//	if v.MaxWithdrawal <= 0 {
//	    return errs.New(errs.InvalidArgument, ErrBadLimit)
//	}
//	if err := limits.Save(ctx, v); err != nil {
//	    return err
//	}
//
// # Defaults
//
// A read decodes the stored document over the defaults with encoding/json/v2:
// a field the document lacks keeps its default, and a member the type lacks
// is skipped. So a field added to T with its default, or removed from it,
// needs no migration; a renamed field is a new one, and starts at its default.
// A map of the document is merged into the map of the defaults, a slice
// replaces the slice of the defaults.
//
// # Replicas
//
// Each replica keeps and rereads the settings on its own. A Save shows on the
// replica that made it at once, on the others with their next read: within
// the interval of WithInterval, 30s by default. Of two concurrent Saves the
// last one wins, whole: a Save writes the entire document.
//
// New reads the settings once before it returns. No document stored is not an
// error: the settings are the defaults until the first Save. A failed read is,
// and the service does not start on the defaults in place of the settings it
// was given. Later a failed reread is logged and keeps the settings as they
// were; the next one tries again.
//
// # Table
//
// The table settings holds a row per name: the document in value, the time of
// its last Save in updated_at. The document is plain JSON:
//
//	SELECT value->>'maintenance' FROM settings WHERE name = 'limits';
//
// # Options
//
//   - WithInterval(d): how often the settings are reread (default 30s).
package settings
