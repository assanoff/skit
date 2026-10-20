// Package translator provides a ready i18n.Translator for dim: Resource builds
// it from the message files of the service.
//
// # Usage
//
// The service supplies its default language and its message files, typically
// an embed.FS of its own catalogs:
//
//	//go:embed locales/*.json
//	var localesFS embed.FS
//
//	// in an app initializer (app.Initializer[Deps]):
//	c.Translator, cleanup = dim.NewResource("Translator",
//	    translator.Resource(localesFS, i18n.Config{
//	        DefaultLang: "ru",
//	        Files:       []string{"locales/ru.json", "locales/kk.json"},
//	    }))
//	return cleanup, nil
//
// i18n.Config is passed through unchanged: see the i18n package for its
// fields.
package translator
