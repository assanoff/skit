package i18n_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing/fstest"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/i18n"
)

// catalogs stand for an embed.FS of locales/*.json.
var catalogs = fstest.MapFS{
	"locales/en.json": {Data: []byte(`{"widget_not_found": "widget {{.id}} not found"}`)},
	"locales/ru.json": {Data: []byte(`{"widget_not_found": "виджет {{.id}} не найден"}`)},
}

// TranslateError localizes an *errs.Error by its MessageID with its Args; a
// language without a catalog falls back to the default one.
func Example() {
	tr, err := i18n.New(catalogs, i18n.Config{
		DefaultLang: "en",
		Files:       []string{"locales/en.json", "locales/ru.json"},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	e := errs.Newf(errs.NotFound, "widget 42 not found").
		WithMessageID("widget_not_found").
		WithArgs(map[string]any{"id": 42})

	fmt.Println(tr.DefaultLang())
	fmt.Println(tr.TranslateError("ru", e).Message)
	fmt.Println(tr.TranslateError("de", e).Message)
	// Output:
	// en
	// виджет 42 не найден
	// widget 42 not found
}

// Middleware stores the language of the request in its context: X-Language
// wins, else the first Accept-Language tag, as a base code.
func ExampleTranslator_Middleware() {
	tr, _ := i18n.New(catalogs, i18n.Config{DefaultLang: "en"})
	h := tr.Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		fmt.Println(i18n.LangFromContext(r.Context()))
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Language", "kk-KZ,ru;q=0.8")
	h.ServeHTTP(httptest.NewRecorder(), r)

	r.Header.Set("X-Language", "ru")
	h.ServeHTTP(httptest.NewRecorder(), r)
	// Output:
	// kk
	// ru
}

// Match picks the best language with a catalog for an Accept-Language value.
func ExampleTranslator_Match() {
	tr, _ := i18n.New(catalogs, i18n.Config{
		DefaultLang: "en",
		Files:       []string{"locales/en.json", "locales/ru.json"},
	})
	fmt.Println(tr.Match("kk-KZ,ru;q=0.8"))
	fmt.Println(tr.Match("de"))
	// Output:
	// ru
	// en
}
