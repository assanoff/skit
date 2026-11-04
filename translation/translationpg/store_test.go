package translationpg_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/dbtest"
	"github.com/assanoff/skit/translation"
	"github.com/assanoff/skit/translation/translationpg"
)

// TestCheckTranslationsExist covers what an admin panel needs to tell the
// operator what to fill in: which columns have no translation and in which
// language.
func TestCheckTranslationsExist(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires docker")
	}

	is := is.New(t)
	ctx := context.Background()

	pg := dbtest.NewPostgres(ctx, t, dbtest.Config{})
	log := slog.New(slog.DiscardHandler)
	store := translationpg.NewStore(log, pg.DB)
	is.NoErr(store.EnsureSchema(ctx)) // the translations table must exist

	const (
		modelName = "settings"
		keyID     = "1"
	)
	columns := []string{"title", "label_title"}
	langs := []translation.Language{translation.LanguageKk}

	save := func(t *testing.T, lang translation.Language, values map[string]string) {
		t.Helper()

		is := is.New(t)
		err := store.SaveTranslation(ctx, translation.Data{
			ModelName: modelName,
			KeyID:     keyID,
			Language:  lang,
			Columns:   values,
		})
		is.NoErr(err) // translations must be saved
	}

	missing := func(t *testing.T) map[string][]string {
		t.Helper()

		is := is.New(t)

		err := store.CheckTranslationsExist(ctx, modelName, keyID, columns, langs)
		is.True(errors.Is(err, translation.ErrMissingTranslations)) // the check must report missing translations

		var missingErr *translation.MissingTranslationsError
		is.True(errors.As(err, &missingErr)) // the error must carry the details

		return missingErr.Missing
	}

	// The default language alone is not enough, every column is still missing.
	save(t, translation.LanguageRu, map[string]string{"title": "Заголовок", "label_title": "Метка"})
	is.Equal(missing(t), map[string][]string{"title": {"kk"}, "label_title": {"kk"}})

	// An empty translation counts as no translation.
	save(t, translation.LanguageKk, map[string]string{"title": "", "label_title": "Белгі"})
	is.Equal(missing(t), map[string][]string{"title": {"kk"}})

	// Everything is filled in.
	save(t, translation.LanguageKk, map[string]string{"title": "Атауы"})
	is.NoErr(store.CheckTranslationsExist(ctx, modelName, keyID, columns, langs))
}

// TestGetAndDelete covers the reads of the middleware, one model and a page
// of them, and the removal of a language.
func TestGetAndDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires docker")
	}

	is := is.New(t)
	ctx := context.Background()

	pg := dbtest.NewPostgres(ctx, t, dbtest.Config{})
	store := translationpg.NewStore(slog.New(slog.DiscardHandler), pg.DB)
	is.NoErr(store.EnsureSchema(ctx))

	kk := translation.LanguageKk
	for id, title := range map[string]string{"1": "Атауы", "2": "Тақырып"} {
		is.NoErr(store.SaveTranslation(ctx, translation.Data{
			ModelName: "product",
			KeyID:     id,
			Language:  kk,
			Columns:   map[string]string{"title": title},
		}))
	}

	got, err := store.GetTranslations(ctx, "product", "1", kk)
	is.NoErr(err)
	is.Equal(got, map[string]string{"title": "Атауы"})
	_, err = store.GetTranslations(ctx, "product", "1", translation.LanguageRu)
	is.True(errors.Is(err, translation.ErrTranslationNotFound)) // no Russian one

	batch, err := store.GetTranslationsBatch(ctx, "product", []string{"1", "2", "3"}, kk)
	is.NoErr(err)
	is.Equal(batch, map[string]map[string]string{ // 3 has none
		"1": {"title": "Атауы"},
		"2": {"title": "Тақырып"},
	})
	batch, err = store.GetTranslationsBatch(ctx, "product", nil, kk)
	is.NoErr(err)
	is.Equal(len(batch), 0)

	is.NoErr(store.DeleteTranslations(ctx, "product", "1", kk))
	_, err = store.GetTranslations(ctx, "product", "1", kk)
	is.True(errors.Is(err, translation.ErrTranslationNotFound)) // deleted
	err = store.DeleteTranslations(ctx, "product", "1", kk)
	is.True(errors.Is(err, translation.ErrTranslationNotFound)) // nothing left
}
