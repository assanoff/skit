package translation

import "time"

// Language identifies a language by its code (and an optional display name).
type Language struct {
	Code string `json:"code"` // BCP-47 / ISO 639-1 code, e.g. "ru", "kk", "en"
	Name string `json:"name"` // human-readable name, e.g. "Russian"
}

// Predefined languages offered as a convenience; applications are free to
// declare their own. The SDK does not assume any particular language set.
var (
	LanguageRu = Language{Code: "ru", Name: "Russian"}
	LanguageKk = Language{Code: "kk", Name: "Kazakh"}
	LanguageEn = Language{Code: "en", Name: "English"}
)

// String returns the language code.
func (l Language) String() string { return l.Code }

// Translatable is implemented by models that carry translatable fields. The
// returned modelName groups instances of the same type; keyID identifies the
// instance.
type Translatable interface {
	GetTranslationKey() (modelName, keyID string)
}

// TranslatableList is implemented by response payloads that wrap a collection of
// Translatable models, so the REST middleware can translate them in one batch
// without per-handler code.
type TranslatableList interface {
	Translatables() []Translatable
}

// Data is a unit of translation to persist for one model instance in
// one language: a set of column -> value pairs.
type Data struct {
	ModelName string
	KeyID     string
	Language  Language
	Columns   map[string]string // column_name -> translation_value
	CreatedAt time.Time
	UpdatedAt time.Time
}

// fieldInfo holds parsed information about a translatable field.
type fieldInfo struct {
	fieldName  string
	columnName string
	isPrimary  bool
	value      string
}
