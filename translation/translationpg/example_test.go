package translationpg_test

import (
	"context"
	"log/slog"

	"github.com/assanoff/skit/dbx/pg"
	"github.com/assanoff/skit/translation"
	"github.com/assanoff/skit/translation/translationpg"
)

// The store keeps a row per translated column of a model; the Translator over
// it (translation.New) reads and writes them by the tags of the model. The
// example needs Postgres, so go test builds it but does not run it.
func Example() {
	ctx := context.Background()
	log := slog.Default()
	db, err := pg.Open(pg.Config{Host: "localhost:5432", Name: "shop", DisableTLS: true})
	if err != nil {
		log.ErrorContext(ctx, "open", "err", err)
		return
	}
	defer func() { _ = db.Close() }()

	store := translationpg.NewStore(log, db)
	if err := store.EnsureSchema(ctx); err != nil {
		log.ErrorContext(ctx, "schema", "err", err)
		return
	}

	err = store.SaveTranslation(ctx, translation.Data{
		ModelName: "products",
		KeyID:     "p-1",
		Language:  translation.LanguageKk,
		Columns:   map[string]string{"title": "Тісті доңғалақ"},
	})
	if err != nil {
		log.ErrorContext(ctx, "save", "err", err)
		return
	}

	// One query for a page of products: key -> column -> translation.
	byKey, err := store.GetTranslationsBatch(ctx, "products",
		[]string{"p-1", "p-2"}, translation.LanguageKk)
	if err != nil {
		log.ErrorContext(ctx, "batch", "err", err)
		return
	}
	log.InfoContext(ctx, "translated", "p-1", byKey["p-1"]["title"], "keys", len(byKey))
}
