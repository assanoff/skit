package translation

import (
	"context"
	"fmt"
	"time"
)

// Translator active object holds all deps
type Translator struct {
	store          Store
	defaultLang    Language
	supportedLangs []Language
}

// New creates a new Translator instance
func New(cfg Config) (*Translator, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	t := &Translator{
		store:          cfg.Store,
		defaultLang:    cfg.DefaultLanguage,
		supportedLangs: cfg.SupportedLangs,
	}

	return t, nil
}

// DefaultLanguage returns the default language
func (t *Translator) DefaultLanguage() Language {
	return t.defaultLang
}

// Languages returns all supported languages by the app
func (t *Translator) Languages() []Language {
	return t.supportedLangs
}

// IsDefaultLanguage checks if the given language is the default language
func (t *Translator) IsDefaultLanguage(lang Language) bool {
	return lang.Code == t.defaultLang.Code
}

// Match maps a language code (e.g. one already resolved by an upstream i18n
// middleware) to a supported Language, falling back to the default when the code
// is empty or unsupported.
func (t *Translator) Match(code string) Language {
	if lang, err := t.findLanguage(code); err == nil {
		return lang
	}
	return t.defaultLang
}

// ValidateLanguage checks if the language is supported
func (t *Translator) ValidateLanguage(lang Language) error {
	for _, supported := range t.supportedLangs {
		if supported.Code == lang.Code {
			return nil
		}
	}
	return ErrInvalidLanguage
}

// Translate translates a single model to the specified language
// Also translates nested Translatable objects in slice fields
func (t *Translator) Translate(ctx context.Context, model Translatable, lang Language) error {
	if t.IsDefaultLanguage(lang) {
		return nil
	}

	if err := t.ValidateLanguage(lang); err != nil {
		return err
	}

	// Use translateMany to handle nested Translatable objects
	return t.translateMany(ctx, []Translatable{model}, lang)
}

// TranslateSlice translates a slice of models with type safety using generics
func TranslateSlice[T Translatable](ctx context.Context, t *Translator, models []T, lang Language) error {
	if len(models) == 0 {
		return nil
	}

	translatables := make([]Translatable, len(models))
	for i, model := range models {
		translatables[i] = model
	}

	return t.translateMany(ctx, translatables, lang)
}

// TranslateMany translates multiple models to the specified language
// Also translates nested Translatable objects in slice fields
func (t *Translator) translateMany(ctx context.Context, models []Translatable, lang Language) error {
	if t.IsDefaultLanguage(lang) {
		return nil
	}

	if err := t.ValidateLanguage(lang); err != nil {
		return err
	}

	if len(models) == 0 {
		return nil
	}

	// Collect all models including nested Translatable objects
	allModels := make([]Translatable, 0, len(models))
	allModels = append(allModels, models...)

	// Collect nested Translatable objects from slice fields
	for _, model := range models {
		nested := collectNestedTranslatables(model)
		allModels = append(allModels, nested...)
	}

	// Group models by model name
	modelGroups := make(map[string][]Translatable)
	for _, model := range allModels {
		modelName, _ := model.GetTranslationKey()
		modelGroups[modelName] = append(modelGroups[modelName], model)
	}

	// Translate each group
	for modelName, group := range modelGroups {
		// Collect key IDs
		keyIDs := make([]string, len(group))
		for i, model := range group {
			_, keyID := model.GetTranslationKey()
			keyIDs[i] = keyID
		}

		batchTranslations, err := t.store.GetTranslationsBatch(ctx, modelName, keyIDs, lang)
		if err != nil {
			return fmt.Errorf("failed to get batch translations: %w", err)
		}

		for _, model := range group {
			_, keyID := model.GetTranslationKey()
			translations, exists := batchTranslations[keyID]
			if !exists {
				continue
			}

			fields, err := parseTranslateTags(model)
			if err != nil {
				return fmt.Errorf("failed to parse translate tags: %w", err)
			}

			if err := t.applyTranslations(model, fields, translations); err != nil {
				return err
			}
		}
	}

	return nil
}

// Save saves translations for a model
func (t *Translator) Save(ctx context.Context, lang Language, model Translatable) error {
	if err := t.ValidateLanguage(lang); err != nil {
		return err
	}

	modelName, keyID := model.GetTranslationKey()

	fields, err := parseTranslateTags(model)
	if err != nil {
		return fmt.Errorf("failed to parse translate tags: %w", err)
	}

	// Build columns map (excluding primary key)
	// key is the column name, value is the field value
	columns := make(map[string]string)
	for _, field := range fields {
		if !field.isPrimary {
			columns[field.columnName] = field.value
		}
	}

	if len(columns) == 0 {
		return fmt.Errorf("no translatable fields found")
	}

	now := time.Now().UTC()
	data := Data{
		ModelName: modelName,
		KeyID:     keyID,
		Language:  lang,
		Columns:   columns,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := t.store.SaveTranslation(ctx, data); err != nil {
		return fmt.Errorf("failed to save translation: %w", err)
	}

	return nil
}

// Get retrieves translations for a model
func (t *Translator) Get(ctx context.Context, lang Language, model Translatable) error {
	if err := t.ValidateLanguage(lang); err != nil {
		return err
	}

	modelName, keyID := model.GetTranslationKey()

	translations, err := t.store.GetTranslations(ctx, modelName, keyID, lang)
	if err != nil {
		return err
	}
	fields, err := parseTranslateTags(model)
	if err != nil {
		return fmt.Errorf("failed to parse translate tags: %w", err)
	}

	return t.applyTranslations(model, fields, translations)
}

// Delete deletes translations for a model
func (t *Translator) Delete(ctx context.Context, lang Language, model Translatable) error {
	if err := t.ValidateLanguage(lang); err != nil {
		return err
	}

	modelName, keyID := model.GetTranslationKey()

	if err := t.store.DeleteTranslations(ctx, modelName, keyID, lang); err != nil {
		return fmt.Errorf("failed to delete translations: %w", err)
	}

	return nil
}

// applyTranslations applies translations to model fields
func (t *Translator) applyTranslations(model Translatable, fields []fieldInfo, translations map[string]string) error {
	for _, field := range fields {
		if field.isPrimary {
			continue
		}

		translationValue, exists := translations[field.columnName]
		if !exists {
			// No translation for this field, keep original value
			continue
		}

		if err := setFieldValue(model, field.fieldName, translationValue); err != nil {
			return fmt.Errorf("failed to set field %s: %w", field.fieldName, err)
		}
	}

	return nil
}
