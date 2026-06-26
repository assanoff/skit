package translation

import (
	"context"
	"fmt"
)

// ValidateTranslateTags validates that the model has correct translate tags
func (t *Translator) ValidateTranslateTags(model Translatable) error {
	_, err := parseTranslateTags(model)
	if err != nil {
		return fmt.Errorf("invalid translate tags: %w", err)
	}

	modelName, keyID := model.GetTranslationKey()
	if modelName == "" {
		return fmt.Errorf("GetTranslationKey() returned empty modelName")
	}
	if keyID == "" {
		return fmt.Errorf("GetTranslationKey() returned empty keyID")
	}

	return nil
}

// CheckTranslationsExist checks if translations exist for all supported languages
func (t *Translator) CheckTranslationsExist(ctx context.Context, model Translatable) error {
	modelName, keyID := model.GetTranslationKey()

	fields, err := parseTranslateTags(model)
	if err != nil {
		return fmt.Errorf("failed to parse translate tags: %w", err)
	}

	var columns []string
	for _, field := range fields {
		if !field.isPrimary {
			columns = append(columns, field.columnName)
		}
	}

	if len(columns) == 0 {
		// No translatable columns, nothing to check
		return nil
	}

	// Get languages to check  except default
	var langsToCheck []Language
	for _, lang := range t.supportedLangs {
		if lang.Code != t.defaultLang.Code {
			langsToCheck = append(langsToCheck, lang)
		}
	}

	if len(langsToCheck) == 0 {
		// Only default language is supported, nothing to check
		return nil
	}

	err = t.store.CheckTranslationsExist(ctx, modelName, keyID, columns, langsToCheck)
	if err != nil {
		return err
	}

	return nil
}
