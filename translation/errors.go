package translation

import "errors"

var (
	// ErrTranslationNotFound is returned when a translation is not found
	ErrTranslationNotFound = errors.New("translation not found")

	// ErrInvalidLanguage is returned when an invalid language code is provided
	ErrInvalidLanguage = errors.New("invalid language")

	// ErrMissingTranslations is returned when required translations are missing
	ErrMissingTranslations = errors.New("missing required translations")

	// ErrInvalidTag is returned when a translate tag is malformed
	ErrInvalidTag = errors.New("invalid translate tag")

	// ErrNoPrimaryKey is returned when no primary key field is found
	ErrNoPrimaryKey = errors.New("no primary key field found with translate:\"primary\" tag")

	// ErrMultiplePrimaryKeys is returned when multiple primary key fields are found
	ErrMultiplePrimaryKeys = errors.New("multiple primary key fields found")

	// ErrInvalidModel is returned when the model doesn't implement Translatable
	ErrInvalidModel = errors.New("model must implement Translatable interface")
)
