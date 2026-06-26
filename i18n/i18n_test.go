package i18n

import (
	"embed"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/assanoff/skit/errs"
)

//go:embed testdata/en.json testdata/ru.json
var catalogs embed.FS

func newTestTranslator(t *testing.T) *Translator {
	t.Helper()
	tr, err := New("en", catalogs, "testdata/en.json", "testdata/ru.json")
	if err != nil {
		t.Fatalf("new translator: %v", err)
	}
	return tr
}

func TestLocalize(t *testing.T) {
	tr := newTestTranslator(t)

	if got := tr.Localize("ru", "greeting"); got != "Привет" {
		t.Errorf("ru greeting = %q", got)
	}
	if got := tr.Localize("en", "greeting"); got != "Hello" {
		t.Errorf("en greeting = %q", got)
	}
	// Unknown language falls back to the default (en).
	if got := tr.Localize("fr", "greeting"); got != "Hello" {
		t.Errorf("fr greeting fallback = %q, want Hello", got)
	}
	// Unknown id falls back to the id itself.
	if got := tr.Localize("en", "missing"); got != "missing" {
		t.Errorf("missing id = %q, want missing", got)
	}
}

func TestLocalizeData(t *testing.T) {
	tr := newTestTranslator(t)
	if got := tr.LocalizeData("ru", "widget_not_found", map[string]any{"id": "42"}); got != "виджет 42 не найден" {
		t.Errorf("ru templated = %q", got)
	}
	if got := tr.LocalizeData("en", "widget_not_found", map[string]any{"id": "42"}); got != "widget 42 not found" {
		t.Errorf("en templated = %q", got)
	}
}

func TestMatch(t *testing.T) {
	tr := newTestTranslator(t)
	cases := map[string]string{
		"":               "en",
		"ru":             "ru",
		"ru-RU,ru;q=0.9": "ru",
		"fr-FR,fr;q=0.8": "en", // unsupported -> default
		"en-US,en;q=0.9": "en",
		"de-DE,ru;q=0.7": "ru", // first supported wins
	}
	for header, want := range cases {
		if got := tr.Match(header); got != want {
			t.Errorf("Match(%q) = %q, want %q", header, got, want)
		}
	}
}

func TestRequestLang(t *testing.T) {
	cases := []struct {
		name      string
		xLanguage string
		accept    string
		want      string
	}{
		{"x-language wins", "kk", "ru", "kk"},
		{"x-language region normalized", "kk-KZ", "", "kk"},
		{"accept primary tag", "", "kk-KZ,ru;q=0.9", "kk"},
		{"accept simple", "", "ru", "ru"},
		{"none", "", "", ""},
		// An unsupported-by-i18n code is still returned raw: each subsystem maps
		// it to its own set (here translation supports it even if i18n doesn't).
		{"unsupported code preserved", "kk", "", "kk"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if c.xLanguage != "" {
				r.Header.Set("X-Language", c.xLanguage)
			}
			if c.accept != "" {
				r.Header.Set("Accept-Language", c.accept)
			}
			if got := RequestLang(r); got != c.want {
				t.Errorf("RequestLang() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestMiddlewareStoresRequestLang(t *testing.T) {
	tr := newTestTranslator(t)
	var got string
	h := tr.Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = LangFromContext(r.Context())
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Language", "kk") // not an i18n catalog, but stored raw
	h.ServeHTTP(httptest.NewRecorder(), r)
	if got != "kk" {
		t.Fatalf("LangFromContext = %q, want kk", got)
	}
}

func TestTranslateError(t *testing.T) {
	tr := newTestTranslator(t)

	e := errs.Newf(errs.NotFound, "widget 42 not found").
		WithMessageID("widget_not_found").
		WithArgs(map[string]any{"id": "42"})

	got := tr.TranslateError("ru", e)
	if got.Message != "виджет 42 не найден" {
		t.Errorf("translated message = %q", got.Message)
	}
	// Original is not mutated.
	if e.Message != "widget 42 not found" {
		t.Errorf("source error was mutated: %q", e.Message)
	}
	// Code is preserved.
	if got.Code != errs.NotFound {
		t.Errorf("code changed: %v", got.Code)
	}

	// No MessageID and no catalog entry for the code -> message unchanged.
	plain := errs.Newf(errs.Internal, "boom")
	if out := tr.TranslateError("ru", plain); out.Message != "boom" {
		t.Errorf("untranslatable message = %q, want boom", out.Message)
	}
	if tr.TranslateError("ru", nil) != nil {
		t.Error("nil error should translate to nil")
	}
}
