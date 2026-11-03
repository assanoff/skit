// Package translationpg is the Postgres translation.Store: each translatable
// column of a record is a row of the translations table, keyed by (model_name,
// column_name, key_id, language_id). Queries run through the dbx helpers,
// which log them and translate the errors of the driver.
//
// # Usage
//
// Build the store over the pool and provision its table at startup;
// EnsureSchema is safe on several replicas booting at once:
//
//	store := translationpg.NewStore(log, db) // db is a *dbx.DB
//	if err := store.EnsureSchema(ctx); err != nil {
//	    return err
//	}
//	tr, err := translation.New(translation.Config{Store: store, ...})
//
// A store over a transaction (*dbx.Tx) commits the translations with the
// business change of the transaction. Schema returns the DDL for a service
// that keeps the table in its own migrations.
//
// An empty value is not a translation: SaveTranslation removes the row of
// that column instead of storing it empty, so "empty" and "absent" stay the
// same for CheckTranslationsExist.
package translationpg
