package translationtest_test

import (
	"context"
	"fmt"

	"github.com/assanoff/skit/translation"
	"github.com/assanoff/skit/translation/translationtest"
)

// product is a model with one translatable field.
type product struct {
	ID    string `translate:"primary"`
	Title string `translate:"title"`
}

func (p *product) GetTranslationKey() (modelName, keyID string) {
	return "products", p.ID
}

// The in-memory Store backs a Translator in tests: it keeps the columns of a
// model per language, and a model with nothing stored is
// translation.ErrTranslationNotFound, as in the Postgres store.
func Example() {
	ctx := context.Background()
	store := translationtest.NewStore()
	tr, err := translation.New(translation.Config{
		Store:           store,
		DefaultLanguage: translation.LanguageEn,
		SupportedLangs:  []translation.Language{translation.LanguageEn, translation.LanguageKk},
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	err = tr.Save(ctx, translation.LanguageKk, &product{ID: "p-1", Title: "Тісті доңғалақ"})
	fmt.Println("save:", err)
	fmt.Println(store.GetTranslations(ctx, "products", "p-1", translation.LanguageKk))

	p := &product{ID: "p-2"}
	fmt.Println(tr.Get(ctx, translation.LanguageKk, p))
	// Output:
	// save: <nil>
	// map[title:Тісті доңғалақ] <nil>
	// translation not found
}
