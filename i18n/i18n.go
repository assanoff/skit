package i18n

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strings"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"

	"github.com/assanoff/skit/errs"
)

// Translator localizes message ids into the configured languages.
type Translator struct {
	bundle      *goi18n.Bundle
	localizers  map[string]*goi18n.Localizer
	matcher     language.Matcher
	tags        []language.Tag
	defaultLang string
}

// New builds a Translator. defaultLang is the BCP-47 tag used as the bundle's
// fallback and when a requested language is unknown. files are paths within
// fsys to JSON catalogs (e.g. "locales/en.json").
func New(defaultLang string, fsys fs.FS, files ...string) (*Translator, error) {
	defTag, err := language.Parse(defaultLang)
	if err != nil {
		return nil, fmt.Errorf("i18n: invalid default language %q: %w", defaultLang, err)
	}

	bundle := goi18n.NewBundle(defTag)
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)

	localizers := map[string]*goi18n.Localizer{}
	tags := []language.Tag{defTag}
	for _, f := range files {
		mf, err := bundle.LoadMessageFileFS(fsys, f)
		if err != nil {
			return nil, fmt.Errorf("i18n: load %q: %w", f, err)
		}
		lang := mf.Tag.String()
		localizers[lang] = goi18n.NewLocalizer(bundle, lang)
		if mf.Tag != defTag {
			tags = append(tags, mf.Tag)
		}
	}
	// Ensure a localizer exists for the default language even if no file declared it.
	if _, ok := localizers[defTag.String()]; !ok {
		localizers[defTag.String()] = goi18n.NewLocalizer(bundle, defTag.String())
	}

	return &Translator{
		bundle:      bundle,
		localizers:  localizers,
		matcher:     language.NewMatcher(tags),
		tags:        tags,
		defaultLang: defTag.String(),
	}, nil
}

// DefaultLang returns the configured default language tag.
func (t *Translator) DefaultLang() string { return t.defaultLang }

// Localize returns the message for id in lang, falling back to the default
// language and then to id itself.
func (t *Translator) Localize(lang, id string) string {
	return t.LocalizeData(lang, id, nil)
}

// LocalizeData is Localize with template data for placeholders in the message.
func (t *Translator) LocalizeData(lang, id string, data map[string]any) string {
	if loc := t.localizerFor(lang); loc != nil {
		cfg := &goi18n.LocalizeConfig{MessageID: id, TemplateData: data}
		if msg, err := loc.Localize(cfg); err == nil {
			return msg
		}
	}
	return id
}

func (t *Translator) localizerFor(lang string) *goi18n.Localizer {
	if loc, ok := t.localizers[lang]; ok {
		return loc
	}
	return t.localizers[t.defaultLang]
}

// Match resolves the best supported language for an Accept-Language header
// value, returning the default language when none matches.
func (t *Translator) Match(acceptLanguage string) string {
	if acceptLanguage == "" {
		return t.defaultLang
	}
	// The matched tag may be region-qualified (e.g. "ru-RU"); the index points
	// back to the supported base tag we registered, which is what the localizers
	// are keyed by.
	_, idx := language.MatchStrings(t.matcher, acceptLanguage)
	if idx >= 0 && idx < len(t.tags) {
		if lang := t.tags[idx].String(); lang != "" {
			if _, ok := t.localizers[lang]; ok {
				return lang
			}
		}
	}
	return t.defaultLang
}

// TranslateError returns a copy of e whose Message is localized for lang using
// e.MessageID (or e.CodeStr when unset) as the key and e.Args as template data.
// When no translation exists the original message is preserved. e is not
// mutated; nil returns nil.
func (t *Translator) TranslateError(lang string, e *errs.Error) *errs.Error {
	if e == nil {
		return nil
	}
	id := e.MessageID
	if id == "" {
		id = e.CodeStr
	}
	out := *e
	if msg := t.LocalizeData(lang, id, e.Args); msg != id {
		out.Message = msg
	}
	return &out
}

// --- request language in context ---

type ctxKey struct{}

// WithLang stores the resolved language in ctx.
func WithLang(ctx context.Context, lang string) context.Context {
	return context.WithValue(ctx, ctxKey{}, lang)
}

// LangFromContext returns the language stored by Middleware/WithLang, or "".
func LangFromContext(ctx context.Context) string {
	lang, _ := ctx.Value(ctxKey{}).(string)
	return lang
}

// RequestLang resolves the raw requested base language code for r: an explicit
// X-Language header wins, otherwise the highest-priority Accept-Language tag.
// The result is normalized to a base code (e.g. "kk-KZ" -> "kk") but is NOT
// constrained to the loaded catalogs — it is the single source of truth for the
// request language, which each subsystem maps to its own supported set (i18n via
// Match/localizerFor, the translation package via its own Match). Returns "" when
// no language is requested.
func RequestLang(r *http.Request) string {
	if x := strings.TrimSpace(r.Header.Get("X-Language")); x != "" {
		return baseCode(x)
	}
	if al := r.Header.Get("Accept-Language"); al != "" {
		if tags, _, err := language.ParseAcceptLanguage(al); err == nil && len(tags) > 0 {
			return baseCode(tags[0].String())
		}
	}
	return ""
}

// baseCode normalizes a language code or tag to its base ISO 639 code.
func baseCode(code string) string {
	tag, err := language.Parse(code)
	if err != nil {
		return code
	}
	if b, conf := tag.Base(); conf != language.No {
		return b.String()
	}
	return code
}

// Middleware resolves the request language (RequestLang) and stores it in the
// request context for downstream handlers and TranslateError. It is the single
// source of truth for the per-request language: an explicit X-Language header
// wins, otherwise the primary Accept-Language tag. Other features (e.g. the
// translation package) read the result via LangFromContext rather than parsing
// headers again, mapping it to their own supported set.
func (t *Translator) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(WithLang(r.Context(), RequestLang(r))))
		})
	}
}
