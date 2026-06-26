// Package translation provides a type-safe, declarative system for translating
// model content stored per-record (distinct from i18n, which localizes static
// catalog/error strings).
//
// Translatable fields are marked with struct tags, similar to json/db/validate.
// Each model implements the Translatable interface to expose its model name and
// instance key. The Translator reads and writes translations through a Store; the
// Postgres implementation lives in the postgres subpackage, and an in-memory
// MockStore is provided for tests.
//
// # Declaring a model
//
//	type Product struct {
//	    ID          string `json:"id"          translate:"primary"`
//	    Title       string `json:"title"       translate:"title"`
//	    Description string `json:"description" translate:"description"`
//	}
//
//	func (s *Product) GetTranslationKey() (modelName, keyID string) {
//	    return "products", s.ID
//	}
//
// Only the primary key field is required; every other tagged field must be a
// string. Fields without a translate tag are ignored.
//
// # Setup
//
//	store := postgres.NewStore(log, db)
//	tr, err := translation.New(translation.Config{
//	    Store:           store,
//	    DefaultLanguage: translation.LanguageRu,
//	    SupportedLangs:  []translation.Language{translation.LanguageRu, translation.LanguageKk},
//	})
//
// # Automatic translation in REST
//
// The translationrest subpackage installs one middleware that resolves the
// request language and translates the response in place, so handlers only return
// their DTO:
//
//	r := router.New(errorMapper, translationrest.Middleware(log, tr))
//
// A response is translated when its rest.ResponseEncoder is a Translatable (single model)
// or a TranslatableList (collection — translated in one batch query). Translating
// to the default language is a no-op.
//
// # Manual use
//
//	// Single model:
//	err := tr.Translate(ctx, product, translation.LanguageKk)
//	// Batch, type-safe:
//	err := translation.TranslateSlice(ctx, tr, products, translation.LanguageKk)
//	// CRUD on the stored translations:
//	err := tr.Save(ctx, translation.LanguageKk, product)
//	err := tr.Get(ctx, translation.LanguageKk, product)
//	err := tr.Delete(ctx, translation.LanguageKk, product)
//
// # Testing
//
//	tr, _ := translation.New(translation.Config{Store: translation.NewMockStore(), ...})
package translation
