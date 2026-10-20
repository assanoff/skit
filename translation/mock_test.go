package translation

import (
	"context"
	"fmt"
	"maps"
	"sync"
)

// mockStore is an in-memory Store for the tests of this package. They cannot
// import translationtest, which imports translation, so they keep this copy.
type mockStore struct {
	mu           sync.RWMutex
	translations map[string]map[string]string // key: "modelName:keyID:langCode:columnName" -> value
}

// newMockStore creates a new mock store.
func newMockStore() *mockStore {
	return &mockStore{
		translations: make(map[string]map[string]string),
	}
}

// SaveTranslation saves translation data in memory.
func (m *mockStore) SaveTranslation(_ context.Context, data Data) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for columnName, value := range data.Columns {
		key := m.buildKey(data.ModelName, data.KeyID, data.Language.Code)
		if m.translations[key] == nil {
			m.translations[key] = make(map[string]string)
		}
		m.translations[key][columnName] = value
	}

	return nil
}

// GetTranslations retrieves translations from memory.
func (m *mockStore) GetTranslations(_ context.Context, modelName, keyID string, lang Language) (map[string]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := m.buildKey(modelName, keyID, lang.Code)
	translations, exists := m.translations[key]
	if !exists || len(translations) == 0 {
		return nil, ErrTranslationNotFound
	}

	// Return a copy to avoid race conditions
	result := make(map[string]string)
	maps.Copy(result, translations)

	return result, nil
}

// DeleteTranslations deletes translations from memory.
func (m *mockStore) DeleteTranslations(_ context.Context, modelName, keyID string, lang Language) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := m.buildKey(modelName, keyID, lang.Code)
	if _, exists := m.translations[key]; !exists {
		return ErrTranslationNotFound
	}

	delete(m.translations, key)
	return nil
}

// CheckTranslationsExist checks if translations exist for all columns and
// languages. When some are missing it returns a *MissingTranslationsError
// listing them. An empty value counts as no translation, matching the Postgres
// store.
func (m *mockStore) CheckTranslationsExist(_ context.Context, modelName, keyID string, columns []string, langs []Language) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	missing := make(map[string][]string)
	for _, column := range columns {
		for _, lang := range langs {
			translations := m.translations[m.buildKey(modelName, keyID, lang.Code)]
			if translations[column] == "" {
				missing[column] = append(missing[column], lang.Code)
			}
		}
	}

	if len(missing) > 0 {
		return &MissingTranslationsError{Missing: missing}
	}

	return nil
}

// GetTranslationsBatch retrieves translations for multiple model instances.
func (m *mockStore) GetTranslationsBatch(_ context.Context, modelName string, keyIDs []string, lang Language) (map[string]map[string]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]map[string]string)
	for _, keyID := range keyIDs {
		key := m.buildKey(modelName, keyID, lang.Code)
		if translations, exists := m.translations[key]; exists {
			// Return a copy
			result[keyID] = make(map[string]string)
			maps.Copy(result[keyID], translations)
		}
	}

	return result, nil
}

// buildKey builds a storage key.
func (m *mockStore) buildKey(modelName, keyID, langCode string) string {
	return fmt.Sprintf("%s:%s:%s", modelName, keyID, langCode)
}

// newMockTranslator creates a translator with mock store for testing.
func newMockTranslator() *Translator {
	return &Translator{
		store:          newMockStore(),
		defaultLang:    LanguageRu,
		supportedLangs: []Language{LanguageRu, LanguageKk},
	}
}
