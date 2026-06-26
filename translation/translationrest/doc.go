// Package translationrest wires the translation package into a skit REST
// router as a single rest.MidFunc that translates responses automatically.
//
// Install it once as an app middleware; handlers stay translation-agnostic and
// just return their DTO:
//
//	tr, _ := translation.New(translation.Config{Store: store, DefaultLanguage: translation.LanguageRu,
//	    SupportedLangs: []translation.Language{translation.LanguageRu, translation.LanguageKk}})
//
//	r := router.New(errorMapper, translationrest.Middleware(log, tr))
//
// For the middleware to translate a response, the returned rest.ResponseEncoder must
// expose its model(s):
//
//   - a single model implements translation.Translatable (and rest.ResponseEncoder);
//   - a collection implements translation.TranslatableList (returning the
//     contained Translatable models) so they translate in one batch query.
//
// Language resolution: Middleware resolves it from the X-Language /
// Accept-Language headers itself (standalone use). When the app already runs the
// i18n middleware — the single source of truth for the request language — use
// MiddlewareWithLang to read that resolved code instead, so headers are parsed
// once per request:
//
//	r := router.New(
//	    localizeErrors(i18nTr),
//	    translationrest.MiddlewareWithLang(log, tr, i18n.LangFromContext),
//	)
//
// The LangFunc reads the language from the context threaded through the rest
// chain, so the source can be the i18n middleware, an app reqctx getter, or any
// func(context.Context) string.
//
// Translating to the default language is a no-op, and any translation error is
// logged while the original content is served.
package translationrest
