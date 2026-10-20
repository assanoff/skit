// Package translationpg is the Postgres implementation of translation.Store.
// It uses the skit dbx helpers for query logging and error translation, and
// stores every translatable column as a row in the translations table keyed by
// (model_name, column_name, key_id, language_id).
package translationpg

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/translation"
)

// Store implements translation.Store against Postgres.
type Store struct {
	log *slog.Logger
	db  *sqlx.DB
}

// NewStore creates a Postgres-backed Store.
func NewStore(log *slog.Logger, db *sqlx.DB) *Store {
	return &Store{log: log, db: db}
}

// Compile-time check that Store satisfies the domain contract.
var _ translation.Store = (*Store)(nil)

// schemaLockKey keys the transaction-level advisory lock that serializes
// EnsureSchema across replicas booting at once.
var schemaLockKey = dbx.AdvisoryKey("skit.translation.schema")

// Schema returns the DDL for the translations table.
func Schema() string {
	return `
CREATE TABLE IF NOT EXISTS translations (
    model_name        VARCHAR(100) NOT NULL,
    column_name       VARCHAR(100) NOT NULL,
    key_id            VARCHAR(255) NOT NULL,
    language_id       VARCHAR(10)  NOT NULL,
    translation_value TEXT,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (model_name, column_name, key_id, language_id)
);
CREATE INDEX IF NOT EXISTS idx_translation_lookup
    ON translations (model_name, key_id, language_id);`
}

// EnsureSchema creates the translations table if absent. It is safe to call at
// startup, including from several replicas at once: the DDL runs under a
// transaction-scoped advisory lock that auto-releases on commit, so concurrent
// boots serialize instead of racing on CREATE TABLE.
func (s *Store) EnsureSchema(ctx context.Context) error {
	return dbx.EnsureSchema(ctx, s.log, s.db, schemaLockKey, Schema())
}

// SaveTranslation upserts every column of data in a single transaction. An empty
// value is not a translation: it clears any existing row for that column instead
// of storing an empty one, keeping "empty" and "absent" the same so
// CheckTranslationsExist stays consistent.
func (s *Store) SaveTranslation(ctx context.Context, data translation.Data) error {
	now := time.Now().UTC()
	if data.CreatedAt.IsZero() {
		data.CreatedAt = now
	}
	if data.UpdatedAt.IsZero() {
		data.UpdatedAt = now
	}

	const upsert = `
		INSERT INTO translations
			(model_name, column_name, key_id, language_id, translation_value, created_at, updated_at)
		VALUES
			(:model_name, :column_name, :key_id, :language_id, :translation_value, :created_at, :updated_at)
		ON CONFLICT (model_name, column_name, key_id, language_id)
		DO UPDATE SET translation_value = EXCLUDED.translation_value,
		              updated_at = EXCLUDED.updated_at`

	const clearRow = `
		DELETE FROM translations
		WHERE model_name = :model_name AND column_name = :column_name
		  AND key_id = :key_id AND language_id = :language_id`

	return dbx.WithinTran(ctx, s.log, s.db, func(tx *sqlx.Tx) error {
		for column, value := range data.Columns {
			row := translationRow{
				ModelName:        data.ModelName,
				ColumnName:       column,
				KeyID:            data.KeyID,
				LanguageID:       data.Language.Code,
				TranslationValue: value,
				CreatedAt:        data.CreatedAt,
				UpdatedAt:        data.UpdatedAt,
			}
			q := upsert
			if value == "" {
				q = clearRow
			}
			if err := dbx.NamedExecContext(ctx, s.log, tx, q, row); err != nil {
				return fmt.Errorf("save column %s: %w", column, err)
			}
		}
		return nil
	})
}

// GetTranslations returns column_name -> translation_value for one model instance.
func (s *Store) GetTranslations(ctx context.Context, modelName, keyID string, lang translation.Language) (map[string]string, error) {
	data := struct {
		ModelName  string `db:"model_name"`
		KeyID      string `db:"key_id"`
		LanguageID string `db:"language_id"`
	}{ModelName: modelName, KeyID: keyID, LanguageID: lang.Code}

	const q = `
		SELECT column_name, translation_value
		FROM translations
		WHERE model_name = :model_name AND key_id = :key_id AND language_id = :language_id`

	var rows []translationResult
	if err := dbx.NamedQuerySlice(ctx, s.log, s.db, q, data, &rows); err != nil {
		return nil, fmt.Errorf("get translations: %w", err)
	}
	if len(rows) == 0 {
		return nil, translation.ErrTranslationNotFound
	}

	result := make(map[string]string, len(rows))
	for _, r := range rows {
		result[r.ColumnName] = r.TranslationValue
	}
	return result, nil
}

// DeleteTranslations removes all translations for one model instance and language.
func (s *Store) DeleteTranslations(ctx context.Context, modelName, keyID string, lang translation.Language) error {
	data := struct {
		ModelName  string `db:"model_name"`
		KeyID      string `db:"key_id"`
		LanguageID string `db:"language_id"`
	}{ModelName: modelName, KeyID: keyID, LanguageID: lang.Code}

	const q = `
		DELETE FROM translations
		WHERE model_name = :model_name AND key_id = :key_id AND language_id = :language_id`

	n, err := dbx.NamedExecContextRowsAffected(ctx, s.log, s.db, q, data)
	if err != nil {
		return fmt.Errorf("delete translations: %w", err)
	}
	if n == 0 {
		return translation.ErrTranslationNotFound
	}
	return nil
}

// CheckTranslationsExist reports the (column, language) pairs that have no
// non-empty value. When some are missing it returns a
// *translation.MissingTranslationsError listing them, so the caller can tell the
// user what exactly to fill in.
func (s *Store) CheckTranslationsExist(ctx context.Context, modelName, keyID string, columns []string, langs []translation.Language) error {
	if len(columns) == 0 || len(langs) == 0 {
		return nil
	}

	langCodes := make([]string, len(langs))
	for i, l := range langs {
		langCodes[i] = l.Code
	}

	data := struct {
		ModelName   string   `db:"model_name"`
		KeyID       string   `db:"key_id"`
		Columns     []string `db:"columns"`
		LanguageIDs []string `db:"language_ids"`
	}{ModelName: modelName, KeyID: keyID, Columns: columns, LanguageIDs: langCodes}

	const q = `
		SELECT column_name, language_id
		FROM translations
		WHERE model_name = :model_name
		  AND key_id = :key_id
		  AND column_name IN (:columns)
		  AND language_id IN (:language_ids)
		  AND translation_value <> ''`

	var rows []existingRow
	if err := dbx.NamedQuerySliceUsingIn(ctx, s.log, s.db, q, data, &rows); err != nil {
		return fmt.Errorf("query translations: %w", err)
	}

	filled := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		filled[r.ColumnName+"|"+r.LanguageID] = struct{}{}
	}

	missing := make(map[string][]string)
	for _, column := range columns {
		for _, code := range langCodes {
			if _, ok := filled[column+"|"+code]; !ok {
				missing[column] = append(missing[column], code)
			}
		}
	}

	if len(missing) > 0 {
		return &translation.MissingTranslationsError{Missing: missing}
	}
	return nil
}

// GetTranslationsBatch returns keyID -> (column_name -> translation_value) for
// many instances of the same model in one query.
func (s *Store) GetTranslationsBatch(ctx context.Context, modelName string, keyIDs []string, lang translation.Language) (map[string]map[string]string, error) {
	if len(keyIDs) == 0 {
		return map[string]map[string]string{}, nil
	}

	data := struct {
		ModelName  string   `db:"model_name"`
		LanguageID string   `db:"language_id"`
		KeyIDs     []string `db:"key_ids"`
	}{ModelName: modelName, LanguageID: lang.Code, KeyIDs: keyIDs}

	const q = `
		SELECT key_id, column_name, translation_value
		FROM translations
		WHERE model_name = :model_name AND language_id = :language_id
		  AND key_id IN (:key_ids)`

	var rows []batchRow
	if err := dbx.NamedQuerySliceUsingIn(ctx, s.log, s.db, q, data, &rows); err != nil {
		return nil, fmt.Errorf("query batch translations: %w", err)
	}

	result := make(map[string]map[string]string)
	for _, r := range rows {
		if result[r.KeyID] == nil {
			result[r.KeyID] = make(map[string]string)
		}
		result[r.KeyID][r.ColumnName] = r.TranslationValue
	}
	return result, nil
}
