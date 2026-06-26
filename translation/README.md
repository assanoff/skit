# translation

Type-safe, declarative translation of **per-record model content** using struct
tags. Distinct from [`i18n`](../i18n), which localizes static catalog and error
strings — `translation` translates data stored per row (a product title, a widget
description) into the request language.

```
translation/                core: Translator, Store, Config, Translatable, parser, MockStore
├── postgres/               Postgres implementation of Store (+ Schema/EnsureSchema)
└── translationrest/        rest.MidFunc that auto-translates responses
```

## How it works

- Mark translatable fields with `translate:"..."` tags (like `json`/`db`/`validate`).
- The model implements `Translatable` to expose its `(modelName, keyID)`.
- The middleware resolves the request language and translates the response in
  place — handlers stay translation-agnostic.

Canonical content lives in your normal tables in the **default language**;
translations for other languages are stored generically in the `translations`
table keyed by `(model_name, column_name, key_id, language_id)`.

## 1. Declare a model

```go
type Product struct {
    ID          string `json:"id"          translate:"primary"`
    Title       string `json:"title"       translate:"title"`
    Description string `json:"description" translate:"description"`
    AuthorID    int    `json:"author_id"` // untagged -> never translated
}

func (s *Product) GetTranslationKey() (modelName, keyID string) {
    return "products", s.ID
}
```

Only `primary` is required. Every other tagged field must be a `string`.

## 2. Build the translator

```go
store := postgres.NewStore(log, db) // db is *sqlx.DB
tr, err := translation.New(translation.Config{
    Store:           store,
    DefaultLanguage: translation.LanguageRu,
    SupportedLangs:  []translation.Language{translation.LanguageRu, translation.LanguageKk},
})

// Tests / simple boot: create the table under an advisory lock. In production
// prefer a migration (goose serializes those itself).
_ = store.EnsureSchema(ctx)
```

The predefined `LanguageRu`/`LanguageKk`/`LanguageEn` are conveniences; declare
your own `translation.Language{Code, Name}` for any other language.

## 3. Automatic translation (middleware)

Install one app middleware; handlers just return their DTO:

```go
r := router.New(errorMapper, translationrest.Middleware(log, tr))
```

For a response to translate, its `rest.ResponseEncoder` must expose its model(s):

```go
// Single model: the DTO is both Translatable and a rest.ResponseEncoder.
func (h *Handler) get(ctx context.Context, r *http.Request) rest.ResponseEncoder {
    return h.toAppProduct(product) // *Product, implements Encode()
}

// Collection: wrap items and expose them via TranslatableList for one batch query.
type ProductList struct{ Items []*Product `json:"items"` }
func (l ProductList) Translatables() []translation.Translatable {
    out := make([]translation.Translatable, len(l.Items))
    for i, s := range l.Items { out[i] = s }
    return out
}
func (l ProductList) Encode() ([]byte, string, error) { /* json */ }
```

Language is taken from `X-Language`, then `Accept-Language`, then the translator
default. Translating to the default language is a no-op; translation errors are
logged and the original content is served (never fails the request).

## 4. Managing translations (CRUD)

```go
// Save a Kazakh translation (upsert per column):
tr.Save(ctx, translation.LanguageKk, &Product{ID: "123", Title: "Қазақша атауы"})

// Read translations into a model:
tr.Get(ctx, translation.LanguageKk, &Product{ID: "123"})

// Delete:
tr.Delete(ctx, translation.LanguageKk, &Product{ID: "123"})
```

## 5. Manual translation

```go
// Single model:
tr.Translate(ctx, product, translation.LanguageKk)

// Batch (one query per model type), type-safe:
translation.TranslateSlice(ctx, tr, products, translation.LanguageKk)
```

## Validation

```go
// Tags are well-formed (one primary, string fields):
tr.ValidateTranslateTags(&Product{})

// All non-default languages have every translatable column (e.g. before publish):
tr.CheckTranslationsExist(ctx, &Product{ID: "123", Title: "x", Description: "y"})
```

## Testing

```go
tr, _ := translation.New(translation.Config{
    Store:           translation.NewMockStore(),
    DefaultLanguage: translation.LanguageRu,
    SupportedLangs:  []translation.Language{translation.LanguageRu, translation.LanguageKk},
})
tr.Save(ctx, translation.LanguageKk, &Product{ID: "123", Title: "Тест"})
```

## Notes & limitations

- Translatable fields must be strings; the parser rejects other kinds with
  `ErrInvalidTag`.
- Nested structs are walked for tagged fields; empty nested structs contribute
  nothing. Slice fields of `Translatable` are translated recursively.
- `Get`/batch lookups return `ErrTranslationNotFound` only from `Get`; the
  middleware path treats a missing translation as "keep original".
- Language resolution overlaps with `i18n`; if you use both, let one middleware
  set the request language and have the other read it.
```
