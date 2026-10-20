package translationtest

import (
	"context"
	"fmt"
	"maps"
	"sync"

	"github.com/assanoff/skit/translation"
)

// Store is an in-memory translation.Store for tests. The zero value is not
// usable; create one with NewStore.
type Store struct {
	mu           sync.RWMutex
	translations map[string]map[string]string // "model:key:lang" -> column -> value
}

// NewStore creates an empty in-memory store.
func NewStore() *Store {
	return &Store{
		translations: make(map[string]map[string]string),
	}
}

// Compile-time check that Store satisfies the domain contract.
var _ translation.Store = (*Store)(nil)

// SaveTranslation saves translation data in memory.
func (s *Store) SaveTranslation(_ context.Context, data translation.Data) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for columnName, value := range data.Columns {
		key := buildKey(data.ModelName, data.KeyID, data.Language.Code)
		if s.translations[key] == nil {
			s.translations[key] = make(map[string]string)
		}
		s.translations[key][columnName] = value
	}

	return nil
}

// GetTranslations retrieves translations from memory.
func (s *Store) GetTranslations(_ context.Context, modelName, keyID string, lang translation.Language) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	translations, exists := s.translations[buildKey(modelName, keyID, lang.Code)]
	if !exists || len(translations) == 0 {
		return nil, translation.ErrTranslationNotFound
	}

	return maps.Clone(translations), nil
}

// DeleteTranslations deletes translations from memory.
func (s *Store) DeleteTranslations(_ context.Context, modelName, keyID string, lang translation.Language) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := buildKey(modelName, keyID, lang.Code)
	if _, exists := s.translations[key]; !exists {
		return translation.ErrTranslationNotFound
	}

	delete(s.translations, key)
	return nil
}

// CheckTranslationsExist checks if translations exist for all columns and
// languages. When some are missing it returns a
// *translation.MissingTranslationsError listing them. An empty value counts as
// no translation, matching the Postgres store.
func (s *Store) CheckTranslationsExist(_ context.Context, modelName, keyID string, columns []string, langs []translation.Language) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	missing := make(map[string][]string)
	for _, column := range columns {
		for _, lang := range langs {
			translations := s.translations[buildKey(modelName, keyID, lang.Code)]
			if translations[column] == "" {
				missing[column] = append(missing[column], lang.Code)
			}
		}
	}

	if len(missing) > 0 {
		return &translation.MissingTranslationsError{Missing: missing}
	}

	return nil
}

// GetTranslationsBatch retrieves translations for multiple model instances.
func (s *Store) GetTranslationsBatch(_ context.Context, modelName string, keyIDs []string, lang translation.Language) (map[string]map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]map[string]string)
	for _, keyID := range keyIDs {
		key := buildKey(modelName, keyID, lang.Code)
		if translations, exists := s.translations[key]; exists {
			result[keyID] = maps.Clone(translations)
		}
	}

	return result, nil
}

// buildKey builds a storage key.
func buildKey(modelName, keyID, langCode string) string {
	return fmt.Sprintf("%s:%s:%s", modelName, keyID, langCode)
}
