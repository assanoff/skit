package translator_test

import (
	"context"
	"fmt"
	"testing/fstest"

	"github.com/assanoff/skit/dim"
	"github.com/assanoff/skit/i18n"
	"github.com/assanoff/skit/provider/translator"
)

// The translator is a resource of the dependencies of a service, built from
// its catalogs on first use. The MapFS stands in for an embed.FS of
// locales/*.json.
func Example() {
	locales := fstest.MapFS{
		"locales/en.json": {Data: []byte(`{"widget_not_found": "widget {{.id}} not found"}`)},
		"locales/kk.json": {Data: []byte(`{"widget_not_found": "{{.id}} виджеті табылмады"}`)},
	}
	tr, cleanup := dim.NewResource("Translator", translator.Resource(locales, i18n.Config{
		DefaultLang: "en",
		Files:       []string{"locales/en.json", "locales/kk.json"},
	}))
	defer func() { _ = cleanup() }()

	data := map[string]any{"id": "w-1"}
	for _, lang := range []string{"en", "kk", "de"} {
		fmt.Println(lang, tr(context.Background()).LocalizeData(lang, "widget_not_found", data))
	}
	// Output:
	// en widget w-1 not found
	// kk w-1 виджеті табылмады
	// de widget w-1 not found
}
