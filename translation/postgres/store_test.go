package postgres_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/dbtest"
	"github.com/assanoff/skit/logger"
	"github.com/assanoff/skit/translation"
	"github.com/assanoff/skit/translation/postgres"
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
	log := logger.New(io.Discard, logger.Config{Service: "test", Level: logger.LevelError})
	store := postgres.NewStore(log, pg.DB)
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
