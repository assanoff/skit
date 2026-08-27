package translation

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

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

// MissingTranslationsError reports which translatable columns have no
// translation and in which languages, so the caller can tell the user what
// exactly to fill in. It wraps ErrMissingTranslations, so errors.Is keeps
// working.
type MissingTranslationsError struct {
	// Missing maps a column name to the codes of the languages it has no
	// translation for.
	Missing map[string][]string
}

// Error implements the error interface.
func (e *MissingTranslationsError) Error() string {
	return ErrMissingTranslations.Error()
}

// Unwrap returns the sentinel so errors.Is(err, ErrMissingTranslations) works.
func (e *MissingTranslationsError) Unwrap() error {
	return ErrMissingTranslations
}

// Describe renders the missing translations for the end user as
// `"column" - language`, joining several columns with "; " in a stable order.
// Column names and language codes are replaced with the given display names;
// the ones absent from the maps are rendered as is.
func (e *MissingTranslationsError) Describe(columnNames, langNames map[string]string) string {
	columns := slices.Sorted(maps.Keys(e.Missing))

	parts := make([]string, 0, len(columns))
	for _, column := range columns {
		langs := make([]string, 0, len(e.Missing[column]))
		for _, code := range e.Missing[column] {
			langs = append(langs, cmp.Or(langNames[code], code))
		}

		parts = append(parts, fmt.Sprintf("%q - %s",
			cmp.Or(columnNames[column], column), strings.Join(langs, ", ")))
	}

	return strings.Join(parts, "; ")
}
