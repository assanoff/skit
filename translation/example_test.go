package translation_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/assanoff/skit/translation"
	"github.com/assanoff/skit/translation/translationtest"
)

// product is a model with translatable fields: its canonical content is in
// the default language, the translations into the others are in the Store.
type product struct {
	ID          string `json:"id"          translate:"primary"`
	Title       string `json:"title"       translate:"title"`
	Description string `json:"description" translate:"description,omitcheck"`
	Price       int    `json:"price"` // untagged: never translated
}

func (p *product) GetTranslationKey() (modelName, keyID string) {
	return "products", p.ID
}

// newTranslator builds a Translator over the in-memory store: English is the
// default language, Russian and Kazakh are translations.
func newTranslator() (*translation.Translator, error) {
	return translation.New(translation.Config{
		Store:           translationtest.NewStore(),
		DefaultLanguage: translation.LanguageEn,
		SupportedLangs: []translation.Language{
			translation.LanguageEn, translation.LanguageRu, translation.LanguageKk,
		},
	})
}

// Save stores the Kazakh translation of a product; Translate puts it into a
// product read in the default language. A product translated into the
// default language stays as it is, and a field with no translation keeps its
// value.
func Example() {
	ctx := context.Background()
	tr, err := newTranslator()
	if err != nil {
		fmt.Println(err)
		return
	}
	err = tr.Save(ctx, translation.LanguageKk, &product{ID: "p-1", Title: "Тісті доңғалақ"})
	if err != nil {
		fmt.Println(err)
		return
	}

	for _, lang := range []translation.Language{translation.LanguageEn, translation.LanguageKk} {
		p := &product{ID: "p-1", Title: "Gear", Description: "Steel, 12 teeth", Price: 10}
		err := tr.Translate(ctx, p, lang)
		fmt.Println(lang, p.Title, "|", p.Description, "|", p.Price, err)
	}
	// Output:
	// en Gear | Steel, 12 teeth | 10 <nil>
	// kk Тісті доңғалақ | Steel, 12 teeth | 10 <nil>
}

// TranslateSlice translates a page of models in one query per model type; a
// model with no translation keeps its content.
func ExampleTranslator_TranslateSlice() {
	ctx := context.Background()
	tr, err := newTranslator()
	if err != nil {
		fmt.Println(err)
		return
	}
	err = tr.Save(ctx, translation.LanguageRu, &product{ID: "p-1", Title: "Шестерня"})
	if err != nil {
		fmt.Println(err)
		return
	}

	products := []*product{{ID: "p-1", Title: "Gear"}, {ID: "p-2", Title: "Bolt"}}
	err = tr.TranslateSlice(ctx, products, translation.LanguageRu)
	fmt.Println(products[0].Title, products[1].Title, err)
	// Output:
	// Шестерня Bolt <nil>
}

// CheckTranslationsExist reports the required columns that miss a language,
// before a product is published; Describe renders them for the user. The
// description is optional (omitcheck), so it is not checked.
func ExampleTranslator_CheckTranslationsExist() {
	ctx := context.Background()
	tr, err := newTranslator()
	if err != nil {
		fmt.Println(err)
		return
	}
	p := &product{ID: "p-1", Title: "Gear"}
	err = tr.Save(ctx, translation.LanguageRu, &product{ID: "p-1", Title: "Шестерня"})
	if err != nil {
		fmt.Println(err)
		return
	}

	err = tr.CheckTranslationsExist(ctx, p)
	fmt.Println(errors.Is(err, translation.ErrMissingTranslations))
	if missing, ok := errors.AsType[*translation.MissingTranslationsError](err); ok {
		fmt.Println(missing.Describe(
			map[string]string{"title": "Title"},
			map[string]string{"kk": "Kazakh", "ru": "Russian"},
		))
	}

	err = tr.Save(ctx, translation.LanguageKk, &product{ID: "p-1", Title: "Тісті доңғалақ"})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(tr.CheckTranslationsExist(ctx, p))
	// Output:
	// true
	// "Title" - Kazakh
	// <nil>
}

// LanguageFromRequest takes the language from X-Language, then from
// Accept-Language, then falls back to the default; LanguageFrom reads the
// headers it is given, here a header of the gateway ahead of the canonical
// ones.
func ExampleTranslator_LanguageFromRequest() {
	tr, err := newTranslator()
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, h := range [][2]string{
		{"X-Language", "kk"},
		{"Accept-Language", "ru-RU,ru;q=0.9,en;q=0.8"},
		{"Accept-Language", "de"},
		{"X-Gateway-Lang", "kk"},
	} {
		r := httptest.NewRequest(http.MethodGet, "/products/p-1", nil)
		r.Header.Set(h[0], h[1])
		fmt.Println(h[0], h[1], "->", tr.LanguageFromRequest(r))
	}

	r := httptest.NewRequest(http.MethodGet, "/products/p-1", nil)
	r.Header.Set("X-Gateway-Lang", "kk")
	sources := append([]translation.HeaderSource{{Name: "X-Gateway-Lang"}},
		translation.DefaultHeaderSources()...)
	fmt.Println("LanguageFrom ->", tr.LanguageFrom(r, sources...))
	// Output:
	// X-Language kk -> kk
	// Accept-Language ru-RU,ru;q=0.9,en;q=0.8 -> ru
	// Accept-Language de -> en
	// X-Gateway-Lang kk -> en
	// LanguageFrom -> kk
}
