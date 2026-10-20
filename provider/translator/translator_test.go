package translator_test

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/matryer/is"

	"github.com/assanoff/skit/i18n"
	"github.com/assanoff/skit/provider/translator"
)

var catalogs = fstest.MapFS{
	"locales/en.json": {Data: []byte(`{"greeting": "Hello"}`)},
	"locales/ru.json": {Data: []byte(`{"greeting": "Привет"}`)},
}

func TestResource(t *testing.T) {
	is := is.New(t)
	cfg := i18n.Config{DefaultLang: "en", Files: []string{"locales/en.json", "locales/ru.json"}}

	tr, cleanup, err := translator.Resource(catalogs, cfg)(context.Background())
	is.NoErr(err)
	is.True(cleanup == nil) // no external resource
	is.Equal(tr.Localize("ru", "greeting"), "Привет")
	is.Equal(tr.Localize("de", "greeting"), "Hello") // the default language
}

func TestResourceBadLanguage(t *testing.T) {
	is := is.New(t)
	tr, _, err := translator.Resource(catalogs, i18n.Config{DefaultLang: "!!"})(context.Background())
	is.True(err != nil)
	is.True(tr == nil)
}
