package translationpg

import "time"

// translationRow is the row representation for upserts.
//
//exhaustruct:enforce
type translationRow struct {
	ModelName        string    `db:"model_name"`
	ColumnName       string    `db:"column_name"`
	KeyID            string    `db:"key_id"`
	LanguageID       string    `db:"language_id"`
	TranslationValue string    `db:"translation_value"`
	CreatedAt        time.Time `db:"created_at"`
	UpdatedAt        time.Time `db:"updated_at"`
}

// translationResult is the select projection for a single-model lookup.
type translationResult struct {
	ColumnName       string `db:"column_name"`
	TranslationValue string `db:"translation_value"`
}

// existingRow is the select projection used to find which (column, language)
// pairs already have a translation.
type existingRow struct {
	ColumnName string `db:"column_name"`
	LanguageID string `db:"language_id"`
}

// batchRow is the select projection for batch lookups.
type batchRow struct {
	KeyID            string `db:"key_id"`
	ColumnName       string `db:"column_name"`
	TranslationValue string `db:"translation_value"`
}
