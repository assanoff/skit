package translation

import (
	"context"
	"net/http"
)

// contextKey is a private context key type to avoid collisions.
type contextKey string

const languageKey contextKey = "translation:language"

// LanguageFromRequest resolves the request language from the X-Language header,
// falling back to Accept-Language and then the translator's default. Unsupported
// codes fall back to the default.
func (t *Translator) LanguageFromRequest(r *http.Request) Language {
	code := r.Header.Get("X-Language")
	if code == "" {
		code = r.Header.Get("Accept-Language")
	}
	if code == "" {
		return t.defaultLang
	}
	if lang, err := t.findLanguage(code); err == nil {
		return lang
	}
	return t.defaultLang
}

// HTTPMiddleware is net/http middleware that resolves the request language and
// stores it in the context for downstream handlers (and the REST translation
// middleware) to read via LanguageFromContext.
func (t *Translator) HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := SetLanguageInContext(r.Context(), t.LanguageFromRequest(r))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// LanguageFromContext returns the language stored by the middleware. The zero
// Language is returned when none is set; callers that need a concrete default
// should compare against Translator.DefaultLanguage.
func LanguageFromContext(ctx context.Context) Language {
	lang, _ := ctx.Value(languageKey).(Language)
	return lang
}

// SetLanguageInContext returns a child context carrying lang.
func SetLanguageInContext(ctx context.Context, lang Language) context.Context {
	return context.WithValue(ctx, languageKey, lang)
}

// findLanguage returns the supported language matching code, or an error.
func (t *Translator) findLanguage(code string) (Language, error) {
	for _, lang := range t.supportedLangs {
		if lang.Code == code {
			return lang, nil
		}
	}
	return t.defaultLang, ErrInvalidLanguage
}
