// Package i18n is a thin wrapper over nicksnyder/go-i18n that loads message
// catalogs from an fs.FS (typically an embed.FS), resolves the request language
// from an Accept-Language header, and localizes *errs.Error messages by their
// MessageID/Code with Args as template data.
//
// Catalogs are JSON files named by BCP-47 tag, e.g. en.json, ru.json. Lookups
// fall back to the default language and then to the raw message id, so a missing
// translation degrades gracefully rather than erroring.
//
// # Usage
//
// Embed the catalogs, build a Translator with a default language, install the
// Middleware to resolve the request language, then localize errors per request:
//
//	//go:embed locales/*.json
//	var locales embed.FS
//
//	tr, err := i18n.New("en", locales, "locales/en.json", "locales/ru.json")
//	if err != nil {
//	    return err
//	}
//
//	mux.Handle("/", tr.Middleware()(handler)) // sets request lang in context
//
//	// In a handler, localize an *errs.Error for the resolved language:
//	lang := i18n.LangFromContext(r.Context())
//	e := tr.TranslateError(lang, errs.From(err))
//	body, contentType, _ := e.Encode()
//
// TranslateError keys off e.MessageID (or e.CodeStr when unset) and feeds e.Args
// as template data; it returns a copy and never mutates the input. For plain
// strings use Localize / LocalizeData directly.
//
// # Language resolution
//
// Match picks the best supported language for an Accept-Language header value,
// returning DefaultLang when nothing matches. Middleware calls Match and stores
// the result via WithLang; LangFromContext reads it back downstream. New always
// registers a localizer for the default language even if no catalog file
// declares it.
package i18n
