package translation

import (
	"context"
	"errors"
	"testing"

	"github.com/matryer/is"
)

// optionalFieldModel has a required and an optional translatable field.
type optionalFieldModel struct {
	ID     string `translate:"primary"`
	Title  string `translate:"title"`
	Poster string `translate:"poster,omitcheck"`
}

func (o *optionalFieldModel) GetTranslationKey() (modelName, keyID string) {
	return "optional", o.ID
}

// newTestTranslator returns a translator together with the store behind it, so a
// test can seed raw rows that Save itself would never produce.
func newTestTranslator() (*Translator, *MockStore) {
	store := NewMockStore()

	return &Translator{
		store:          store,
		defaultLang:    LanguageRu,
		supportedLangs: []Language{LanguageRu, LanguageKk},
	}, store
}

func TestParseTranslateTag(t *testing.T) {
	tests := []struct {
		name          string
		tag           string
		wantName      string
		wantOmitCheck bool
		wantErr       bool
	}{
		{name: "no tag", tag: "", wantName: ""},
		{name: "primary", tag: "primary", wantName: "primary"},
		{name: "plain column", tag: "title", wantName: "title"},
		{name: "omitcheck", tag: "poster,omitcheck", wantName: "poster", wantOmitCheck: true},
		{name: "space before option", tag: "poster, omitcheck", wantName: "poster", wantOmitCheck: true},
		{name: "spaces everywhere", tag: " poster , omitcheck ", wantName: "poster", wantOmitCheck: true},
		{name: "trailing comma", tag: "poster,", wantName: "poster"},
		{name: "doubled comma", tag: "poster,,omitcheck", wantName: "poster", wantOmitCheck: true},
		{name: "unknown option", tag: "poster,unknown", wantErr: true},
		{name: "misspelled option", tag: "poster,omitCheck", wantErr: true},
		{name: "option without column name", tag: ",omitcheck", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			name, omitCheck, err := parseTranslateTag(tt.tag)
			if tt.wantErr {
				is.True(errors.Is(err, ErrInvalidTag)) // a malformed tag must be rejected
				return
			}

			is.NoErr(err)                         // a well-formed tag must parse
			is.Equal(name, tt.wantName)           // column name
			is.Equal(omitCheck, tt.wantOmitCheck) // omitcheck option
		})
	}
}

// badTagModel misspells the option, which would silently turn the field back
// into a required one if unknown options were ignored.
type badTagModel struct {
	ID     string `translate:"primary"`
	Poster string `translate:"poster,omitCheck"`
}

func (b *badTagModel) GetTranslationKey() (modelName, keyID string) {
	return "bad", b.ID
}

// TestParseTranslateTagsRejectsUnknownOption makes sure a typo in the tag fails
// loudly instead of being ignored.
func TestParseTranslateTagsRejectsUnknownOption(t *testing.T) {
	is := is.New(t)

	_, err := parseTranslateTags(&badTagModel{ID: "1", Poster: "poster.jpg"})
	is.True(errors.Is(err, ErrInvalidTag)) // an unknown option must be an error
}

// TestParseTranslateTagsOptional makes sure the option does not leak into the
// column name and that only the tagged field is marked optional.
func TestParseTranslateTagsOptional(t *testing.T) {
	is := is.New(t)

	fields, err := parseTranslateTags(&optionalFieldModel{ID: "1", Title: "Title", Poster: "poster.jpg"})
	is.NoErr(err) // the tags are well-formed

	byColumn := make(map[string]fieldInfo, len(fields))
	for _, f := range fields {
		byColumn[f.columnName] = f
	}

	_, ok := byColumn["poster"]
	is.True(ok)                            // the option must not leak into the column name
	is.True(byColumn["poster"].optional)   // poster is optional
	is.True(!byColumn["title"].optional)   // title stays required
	is.True(!byColumn["primary"].optional) // the primary key is never optional
}

// TestCheckTranslationsExistSkipsOptional covers the whole point of the option:
// a missing optional translation must not block the model.
func TestCheckTranslationsExistSkipsOptional(t *testing.T) {
	ctx := context.Background()

	t.Run("optional translation missing", func(t *testing.T) {
		is := is.New(t)
		tr := NewMockTranslator()
		model := &optionalFieldModel{ID: "1", Title: "Title", Poster: "poster.jpg"}

		// Only the required field is translated.
		err := tr.Save(ctx, LanguageKk, &optionalFieldModel{ID: "1", Title: "Тақырып"})
		is.NoErr(err) // the translation must be saved

		is.NoErr(tr.CheckTranslationsExist(ctx, model)) // an optional field is not required
	})

	t.Run("required translation missing", func(t *testing.T) {
		is := is.New(t)
		tr := NewMockTranslator()
		model := &optionalFieldModel{ID: "1", Title: "Title", Poster: "poster.jpg"}

		err := tr.Save(ctx, LanguageKk, &optionalFieldModel{ID: "1", Poster: "poster_kk.jpg"})
		is.NoErr(err) // the translation must be saved

		err = tr.CheckTranslationsExist(ctx, model)

		var missingErr *MissingTranslationsError
		is.True(errors.As(err, &missingErr)) // the error must carry the details

		_, titleMissing := missingErr.Missing["title"]
		_, posterMissing := missingErr.Missing["poster"]
		is.True(titleMissing)   // the required title is reported
		is.True(!posterMissing) // the optional poster is not
	})
}

// TestOptionalFieldFallback covers reading. The three cases are three genuinely
// different storage states: no poster column at all (what an existing production
// row looks like), a stored empty poster, and a real translation.
func TestOptionalFieldFallback(t *testing.T) {
	ctx := context.Background()
	const defaultPoster = "poster_ru.jpg"

	tests := []struct {
		name       string
		stored     map[string]string
		wantPoster string
	}{
		{
			name:       "no poster column stored",
			stored:     map[string]string{"title": "Тақырып"},
			wantPoster: defaultPoster,
		},
		{
			name:       "poster column stored empty",
			stored:     map[string]string{"title": "Тақырып", "poster": ""},
			wantPoster: defaultPoster,
		},
		{
			name:       "poster translated",
			stored:     map[string]string{"title": "Тақырып", "poster": "poster_kk.jpg"},
			wantPoster: "poster_kk.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)
			tr, store := newTestTranslator()

			err := store.SaveTranslation(ctx, Data{
				ModelName: "optional",
				KeyID:     "1",
				Language:  LanguageKk,
				Columns:   tt.stored,
			})
			is.NoErr(err) // seeding the store must succeed

			model := &optionalFieldModel{ID: "1", Title: "Title", Poster: defaultPoster}
			is.NoErr(tr.Get(ctx, LanguageKk, model)) // reading the translation must succeed

			is.Equal(model.Poster, tt.wantPoster) // optional field
			is.Equal(model.Title, "Тақырып")      // required field is always applied
		})
	}
}

// TestSaveKeepsExistingOptionalTranslation covers writing: a caller that does not
// carry the optional attribute (an older client, or one following the previous
// API contract) must not wipe a translation somebody else has set.
func TestSaveKeepsExistingOptionalTranslation(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	tr, store := newTestTranslator()

	err := tr.Save(ctx, LanguageKk, &optionalFieldModel{ID: "1", Title: "Тақырып", Poster: "poster_kk.jpg"})
	is.NoErr(err) // the first save must succeed

	// The poster attribute is absent from the payload, so it arrives empty.
	err = tr.Save(ctx, LanguageKk, &optionalFieldModel{ID: "1", Title: "Жаңа тақырып"})
	is.NoErr(err) // the second save must succeed

	stored, err := store.GetTranslations(ctx, "optional", "1", LanguageKk)
	is.NoErr(err) // the translations must be readable

	is.Equal(stored["poster"], "poster_kk.jpg") // the optional translation survives
	is.Equal(stored["title"], "Жаңа тақырып")   // the required one is updated
}

// TestSaveWritesRequiredFieldEvenWhenEmpty pins the asymmetry: only optional
// fields are skipped when empty, a required one is still written.
func TestSaveWritesRequiredFieldEvenWhenEmpty(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	tr, store := newTestTranslator()

	err := tr.Save(ctx, LanguageKk, &optionalFieldModel{ID: "1", Title: "", Poster: "poster_kk.jpg"})
	is.NoErr(err) // the save must succeed

	stored, err := store.GetTranslations(ctx, "optional", "1", LanguageKk)
	is.NoErr(err) // the translations must be readable

	_, hasTitle := stored["title"]
	_, hasPoster := stored["poster"]
	is.True(hasTitle)  // an empty required column is still written
	is.True(hasPoster) // the optional column carries a value
}

// TestRequiredFieldEmptyTranslationApplies pins the existing behavior of
// required fields: an empty translation is applied as is.
func TestRequiredFieldEmptyTranslationApplies(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	tr := NewMockTranslator()

	err := tr.Save(ctx, LanguageKk, &optionalFieldModel{ID: "1", Title: ""})
	is.NoErr(err) // the save must succeed

	model := &optionalFieldModel{ID: "1", Title: "Title", Poster: "poster_ru.jpg"}
	is.NoErr(tr.Get(ctx, LanguageKk, model)) // reading the translation must succeed

	is.Equal(model.Title, "") // an empty translation of a required field is applied
}
