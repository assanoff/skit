// Package translationtest provides an in-memory translation.Store for tests,
// so a Translator runs without Postgres.
//
// It follows the Postgres store's semantics: an empty value counts as no
// translation in CheckTranslationsExist, and a lookup with nothing stored
// returns translation.ErrTranslationNotFound.
//
// # Usage
//
//	tr, err := translation.New(translation.Config{
//		Store:           translationtest.NewStore(),
//		DefaultLanguage: translation.LanguageRu,
//		SupportedLangs:  []translation.Language{translation.LanguageRu, translation.LanguageKk},
//	})
//
// # API
//
//   - Store / NewStore: a translation.Store over a map, safe for concurrent use.
package translationtest
