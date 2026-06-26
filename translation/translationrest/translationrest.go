package translationrest

import (
	"context"
	"net/http"

	"github.com/assanoff/skit/logger"
	"github.com/assanoff/skit/rest"
	"github.com/assanoff/skit/translation"
)

// LangFunc resolves the request's language code from the context, typically a
// value placed by an upstream resolver (e.g. i18n.LangFromContext, or an app
// reqctx getter). The context is the one threaded through the rest middleware
// chain, so it sees values set by both net/http and rest.MidFunc middleware.
// Returning "" falls back to the translator's default language.
type LangFunc func(context.Context) string

// Middleware translates response payloads, resolving the request language itself
// from the X-Language / Accept-Language headers. Use this for standalone setups
// where no other middleware resolves the language.
//
// After the handler runs, a response is translated when its ResponseEncoder is a
// translation.Translatable (single model) or translation.TranslatableList
// (collection, translated in one batch query). Translating to the default
// language is a no-op; translation errors are logged and the original content is
// served, so a missing translation never fails the request.
func Middleware(log *logger.Logger, t *translation.Translator) rest.MidFunc {
	return middleware(log, t, func(_ context.Context, r *http.Request) translation.Language {
		return t.LanguageFromRequest(r)
	})
}

// MiddlewareWithLang is like Middleware but takes the request language from lang
// (e.g. the code already resolved by the i18n middleware), so the language is
// resolved once per request instead of parsed twice. Wire it as:
//
//	translationrest.MiddlewareWithLang(log, tr, func(r *http.Request) string {
//	    return i18n.LangFromContext(r.Context())
//	})
func MiddlewareWithLang(log *logger.Logger, t *translation.Translator, lang LangFunc) rest.MidFunc {
	return middleware(log, t, func(ctx context.Context, _ *http.Request) translation.Language {
		return t.Match(lang(ctx))
	})
}

// middleware is the shared core: resolve the language with langOf, run the
// handler, and best-effort translate its payload.
func middleware(log *logger.Logger, t *translation.Translator, langOf func(context.Context, *http.Request) translation.Language) rest.MidFunc {
	return func(next rest.HandlerFunc) rest.HandlerFunc {
		return func(ctx context.Context, r *http.Request) rest.ResponseEncoder {
			lang := langOf(ctx, r)
			ctx = translation.SetLanguageInContext(ctx, lang)

			enc := next(ctx, r)
			if enc == nil || t.IsDefaultLanguage(lang) {
				return enc
			}

			var err error
			switch v := enc.(type) {
			case translation.TranslatableList:
				err = translation.TranslateSlice(ctx, t, v.Translatables(), lang)
			case translation.Translatable:
				err = t.Translate(ctx, v, lang)
			}
			if err != nil && log != nil {
				log.Warn(ctx, "translationrest: translate response", "lang", lang.Code, "error", err)
			}
			return enc
		}
	}
}
