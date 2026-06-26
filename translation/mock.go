package translation

import (
	"context"
	"fmt"
	"maps"
	"sync"
)

// MockStore is a mock implementation of Store for testing
type MockStore struct {
	mu           sync.RWMutex
	translations map[string]map[string]string // key: "modelName:keyID:langCode:columnName" -> value
}

// NewMockStore creates a new mock store
func NewMockStore() *MockStore {
	return &MockStore{
		translations: make(map[string]map[string]string),
	}
}

// SaveTranslation saves translation data in memory
func (m *MockStore) SaveTranslation(ctx context.Context, data Data) error {
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

// GetTranslations retrieves translations from memory
func (m *MockStore) GetTranslations(ctx context.Context, modelName, keyID string, lang Language) (map[string]string, error) {
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

// DeleteTranslations deletes translations from memory
func (m *MockStore) DeleteTranslations(ctx context.Context, modelName, keyID string, lang Language) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := m.buildKey(modelName, keyID, lang.Code)
	if _, exists := m.translations[key]; !exists {
		return ErrTranslationNotFound
	}

	delete(m.translations, key)
	return nil
}

// CheckTranslationsExist checks if translations exist for all columns and languages
func (m *MockStore) CheckTranslationsExist(ctx context.Context, modelName, keyID string, columns []string, langs []Language) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, lang := range langs {
		key := m.buildKey(modelName, keyID, lang.Code)
		translations, exists := m.translations[key]
		if !exists {
			return ErrMissingTranslations
		}

		for _, column := range columns {
			if _, exists := translations[column]; !exists {
				return ErrMissingTranslations
			}
		}
	}

	return nil
}

// GetTranslationsBatch retrieves translations for multiple model instances
func (m *MockStore) GetTranslationsBatch(ctx context.Context, modelName string, keyIDs []string, lang Language) (map[string]map[string]string, error) {
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

// buildKey builds a storage key
func (m *MockStore) buildKey(modelName, keyID, langCode string) string {
	return fmt.Sprintf("%s:%s:%s", modelName, keyID, langCode)
}

// NewMockTranslator creates a translator with mock store for testing
func NewMockTranslator() *Translator {
	return &Translator{
		store:          NewMockStore(),
		defaultLang:    LanguageRu,
		supportedLangs: []Language{LanguageRu, LanguageKk},
	}
}
