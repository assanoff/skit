package audittest

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/assanoff/skit/auditlog"
	"github.com/assanoff/skit/dbx"
)

// Store is an in-memory auditlog.Store for tests and examples. The zero value
// is not usable; create one with NewStore.
type Store struct {
	mu     sync.Mutex
	lastID int
	models map[model][]auditlog.AuditLog // the versions of a model, ascending
}

// model identifies the history of one model.
type model struct {
	typ, id string
}

// NewStore creates an empty in-memory store.
func NewStore() *Store {
	return &Store{models: make(map[model][]auditlog.AuditLog)}
}

// Compile-time check that Store satisfies the domain contract.
var _ auditlog.Store = (*Store)(nil)

// Save stores a version of a model under the next ID. A version the model
// already has fails with dbx.ErrDBDuplicatedEntry, as the UNIQUE constraint of
// the Postgres store does.
func (s *Store) Save(_ context.Context, al auditlog.AuditLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	k := model{typ: al.ModelType, id: al.ModelID}
	i, found := slices.BinarySearchFunc(s.models[k], al.Version, byVersion)
	if found {
		return fmt.Errorf("save %s/%s v%d: %w",
			al.ModelType, al.ModelID, al.Version, dbx.ErrDBDuplicatedEntry)
	}
	s.lastID++
	al.ID = s.lastID
	al.Payload = slices.Clone(al.Payload)
	s.models[k] = slices.Insert(s.models[k], i, al)
	return nil
}

// QueryLastByModelID returns the latest version of a model, or
// auditlog.ErrNotFound.
func (s *Store) QueryLastByModelID(_ context.Context, modelType, modelID string) (auditlog.AuditLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	vers := s.models[model{typ: modelType, id: modelID}]
	if len(vers) == 0 {
		return auditlog.AuditLog{}, auditlog.ErrNotFound
	}
	return vers[len(vers)-1], nil
}

// QueryHistoryByModelID returns every version of a model, ascending.
func (s *Store) QueryHistoryByModelID(_ context.Context, modelType, modelID string) ([]auditlog.AuditLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.models[model{typ: modelType, id: modelID}]), nil
}

// QueryModelByVersion returns one version of a model, or auditlog.ErrNotFound.
func (s *Store) QueryModelByVersion(_ context.Context, modelType, modelID string, ver int) (auditlog.AuditLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	vers := s.models[model{typ: modelType, id: modelID}]
	i, found := slices.BinarySearchFunc(vers, ver, byVersion)
	if !found {
		return auditlog.AuditLog{}, auditlog.ErrNotFound
	}
	return vers[i], nil
}

// Versions returns the version numbers of a model, ascending.
func (s *Store) Versions(_ context.Context, modelType, modelID string) ([]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	vers := s.models[model{typ: modelType, id: modelID}]
	out := make([]int, len(vers))
	for i, al := range vers {
		out[i] = al.Version
	}
	return out, nil
}

// DeleteVersions removes the given versions of a model and returns the count
// deleted.
func (s *Store) DeleteVersions(_ context.Context, modelType, modelID string, versions []int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	k := model{typ: modelType, id: modelID}
	n := len(s.models[k])
	s.models[k] = slices.DeleteFunc(s.models[k], func(al auditlog.AuditLog) bool {
		return slices.Contains(versions, al.Version)
	})
	return n - len(s.models[k]), nil
}

// OverThreshold lists the models with more than threshold versions,
// most-versioned first, up to limit.
func (s *Store) OverThreshold(_ context.Context, threshold, limit int) ([]auditlog.ModelRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var refs []auditlog.ModelRef
	for k, vers := range s.models {
		if len(vers) > threshold {
			refs = append(refs, auditlog.ModelRef{
				ModelType: k.typ, ModelID: k.id, Versions: len(vers),
			})
		}
	}
	slices.SortFunc(refs, func(a, b auditlog.ModelRef) int {
		return cmp.Or(
			cmp.Compare(b.Versions, a.Versions),
			cmp.Compare(a.ModelType, b.ModelType),
			cmp.Compare(a.ModelID, b.ModelID),
		)
	})
	if limit >= 0 && limit < len(refs) {
		refs = refs[:limit]
	}
	return refs, nil
}

// byVersion orders the versions of a model for a binary search.
func byVersion(al auditlog.AuditLog, ver int) int {
	return cmp.Compare(al.Version, ver)
}
