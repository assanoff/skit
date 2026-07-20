package translation

import (
	"context"
	"net/http"

	"golang.org/x/text/language"
)

// contextKey is a private context key type to avoid collisions.
type contextKey string

const languageKey contextKey = "translation:language"

// HeaderSource names a request header to read the language from, and how to
// interpret its value. When AcceptLanguage is true the value is parsed as an
// RFC 7231 Accept-Language list (quality weights, matched on the base subtag so
// "ru-RU" resolves to supported "ru"); otherwise the raw value is treated as a
// single language code and matched exactly. Sources are tried in order — the
// first that yields a supported language wins.
type HeaderSource struct {
	Name           string
	AcceptLanguage bool
}

// DefaultHeaderSources are the canonical language headers, in priority order: an
// explicit X-Language override (exact code) then Accept-Language (parsed). To
// support a custom header, prepend it and keep these as the fallback:
//
//	sources := append([]translation.HeaderSource{{Name: "X-Tenant-Lang"}}, translation.DefaultHeaderSources()...)
func DefaultHeaderSources() []HeaderSource {
	return []HeaderSource{
		{Name: "X-Language"},
		{Name: "Accept-Language", AcceptLanguage: true},
	}
}

// LanguageFrom resolves the request language from the given header sources, in
// order, falling back to the translator's default. With no sources it uses
// DefaultHeaderSources. This is the header-agnostic core behind
// LanguageFromRequest and the language middleware.
func (t *Translator) LanguageFrom(r *http.Request, sources ...HeaderSource) Language {
	if len(sources) == 0 {
		sources = DefaultHeaderSources()
	}
	for _, src := range sources {
		raw := r.Header.Get(src.Name)
		if raw == "" {
			continue
		}
		if src.AcceptLanguage {
			if lang, ok := t.matchAcceptLanguage(raw); ok {
				return lang
			}
			continue
		}
		if lang, err := t.findLanguage(raw); err == nil {
			return lang
		}
	}
	return t.defaultLang
}

// LanguageFromRequest resolves the request language from the canonical language
// headers (DefaultHeaderSources: X-Language then Accept-Language). For custom
// headers use LanguageFrom, or configure the middleware via LanguageMiddleware.
func (t *Translator) LanguageFromRequest(r *http.Request) Language {
	return t.LanguageFrom(r)
}

// matchAcceptLanguage parses an RFC 7231 Accept-Language value and returns the
// first supported language (matched on the base subtag), or false when none match.
func (t *Translator) matchAcceptLanguage(header string) (Language, bool) {
	tags, _, err := language.ParseAcceptLanguage(header)
	if err != nil {
		return Language{}, false
	}
	for _, tag := range tags {
		base, _ := tag.Base()
		if lang, err := t.findLanguage(base.String()); err == nil {
			return lang, true
		}
	}
	return Language{}, false
}

// HTTPMiddleware is net/http middleware that resolves the request language from
// the canonical headers (DefaultHeaderSources) and stores it in the context for
// downstream handlers (and the REST translation middleware) to read via
// LanguageFromContext. For custom headers use LanguageMiddleware.
func (t *Translator) HTTPMiddleware(next http.Handler) http.Handler {
	return t.LanguageMiddleware()(next)
}

// LanguageMiddleware returns net/http middleware that resolves the request
// language from the given header sources — defaulting to DefaultHeaderSources
// when none are passed — and stores it in the context. This keeps the package
// header-agnostic: pass your own HeaderSource list (e.g. a tenant- or
// gateway-specific header) while keeping the canonical headers as the fallback.
func (t *Translator) LanguageMiddleware(sources ...HeaderSource) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := SetLanguageInContext(r.Context(), t.LanguageFrom(r, sources...))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
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
