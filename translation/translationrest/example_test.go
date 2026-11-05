package translationrest_test

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/assanoff/skit/rest"
	"github.com/assanoff/skit/rest/router"
	"github.com/assanoff/skit/translation"
	"github.com/assanoff/skit/translation/translationrest"
	"github.com/assanoff/skit/translation/translationtest"
)

// widget is a translatable DTO that is its own rest.ResponseEncoder.
type widget struct {
	ID    string `json:"id"    translate:"primary"`
	Title string `json:"title" translate:"title"`
}

func (w *widget) GetTranslationKey() (modelName, keyID string) {
	return "widgets", w.ID
}

func (w *widget) Encode() ([]byte, string, error) {
	b, err := json.Marshal(w)
	return b, "application/json", err
}

// widgetList is a collection payload: TranslatableList hands its items to the
// middleware, which translates them in one batch.
type widgetList struct {
	Items []*widget `json:"items"`
}

func (l widgetList) Translatables() []translation.Translatable {
	out := make([]translation.Translatable, len(l.Items))
	for i, w := range l.Items {
		out[i] = w
	}
	return out
}

func (l widgetList) Encode() ([]byte, string, error) {
	b, err := json.Marshal(l)
	return b, "application/json", err
}

// newShopTranslator builds a Translator over the in-memory store, English by
// default, with the Kazakh title of the widget w-1.
func newShopTranslator(ctx context.Context) (*translation.Translator, error) {
	tr, err := translation.New(translation.Config{
		Store:           translationtest.NewStore(),
		DefaultLanguage: translation.LanguageEn,
		SupportedLangs:  []translation.Language{translation.LanguageEn, translation.LanguageKk},
	})
	if err != nil {
		return nil, err
	}
	return tr, tr.Save(ctx, translation.LanguageKk, &widget{ID: "w-1", Title: "Тісті доңғалақ"})
}

// Middleware resolves the language of the request from its headers and
// translates the response of the handler in place: the handler returns its
// DTO in the default language. A language the translator does not support
// falls back to the default.
func Example() {
	tr, err := newShopTranslator(context.Background())
	if err != nil {
		fmt.Println(err)
		return
	}
	r := router.New(translationrest.Middleware(nil, tr))
	r.HandleApp("GET /widgets/{id}", func(_ context.Context, r *http.Request) rest.ResponseEncoder {
		return &widget{ID: r.PathValue("id"), Title: "Gear"}
	})

	for _, lang := range []string{"", "kk-KZ,kk;q=0.9", "de"} {
		req := httptest.NewRequest(http.MethodGet, "/widgets/w-1", nil)
		req.Header.Set("Accept-Language", lang)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		fmt.Printf("%q %s\n", lang, strings.TrimSpace(w.Body.String()))
	}
	// Output:
	// "" {"id":"w-1","title":"Gear"}
	// "kk-KZ,kk;q=0.9" {"id":"w-1","title":"Тісті доңғалақ"}
	// "de" {"id":"w-1","title":"Gear"}
}

// MiddlewareWithLang takes the language another middleware has resolved, here
// the language of the profile of the user; a list response is translated in
// one batch.
func ExampleMiddlewareWithLang() {
	tr, err := newShopTranslator(context.Background())
	if err != nil {
		fmt.Println(err)
		return
	}

	type langKey struct{}
	profileLang := func(next rest.HandlerFunc) rest.HandlerFunc {
		return func(ctx context.Context, r *http.Request) rest.ResponseEncoder {
			return next(context.WithValue(ctx, langKey{}, "kk"), r)
		}
	}
	langOf := func(ctx context.Context) string {
		lang, _ := ctx.Value(langKey{}).(string)
		return lang
	}

	r := router.New(profileLang, translationrest.MiddlewareWithLang(nil, tr, langOf))
	r.HandleApp("GET /widgets", func(context.Context, *http.Request) rest.ResponseEncoder {
		return widgetList{Items: []*widget{{ID: "w-1", Title: "Gear"}, {ID: "w-2", Title: "Bolt"}}}
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/widgets", nil))
	fmt.Println(strings.TrimSpace(w.Body.String()))
	// Output:
	// {"items":[{"id":"w-1","title":"Тісті доңғалақ"},{"id":"w-2","title":"Bolt"}]}
}
