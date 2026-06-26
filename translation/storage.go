package translation

import (
	"context"
)

// Store is the interface for translation storage
type Store interface {
	// SaveTranslation saves translation data (should be executed in a transaction)
	SaveTranslation(ctx context.Context, data Data) error

	// GetTranslations retrieves all translations for a specific model instance and language
	// Returns a map of column_name -> translation_value
	GetTranslations(ctx context.Context, modelName, keyID string, lang Language) (map[string]string, error)

	// DeleteTranslations deletes all translations for a specific model instance and language
	DeleteTranslations(ctx context.Context, modelName, keyID string, lang Language) error

	// CheckTranslationsExist checks if translations exist for all specified columns and languages
	// Returns nil if all translations exist, ErrMissingTranslations otherwise
	CheckTranslationsExist(ctx context.Context, modelName, keyID string, columns []string, langs []Language) error

	// GetTranslationsBatch retrieves translations for multiple model instances
	// Returns a map of keyID -> (column_name -> translation_value)
	GetTranslationsBatch(ctx context.Context, modelName string, keyIDs []string, lang Language) (map[string]map[string]string, error)
}
