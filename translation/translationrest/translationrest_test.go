package translationrest_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/assanoff/skit/rest"
	"github.com/assanoff/skit/translation"
	"github.com/assanoff/skit/translation/translationrest"
)

// product is a translatable DTO that is itself a rest.ResponseEncoder.
type product struct {
	ID    string `json:"id" translate:"primary"`
	Title string `json:"title" translate:"title"`
}

func (s *product) GetTranslationKey() (string, string) { return "products", s.ID }

func (s *product) Encode() ([]byte, string, error) {
	b, err := json.Marshal(s)
	return b, "application/json", err
}

// productList is a collection payload exposing its translatable items.
type productList struct {
	Items []*product `json:"items"`
}

func (l productList) Translatables() []translation.Translatable {
	out := make([]translation.Translatable, len(l.Items))
	for i, s := range l.Items {
		out[i] = s
	}
	return out
}

func (l productList) Encode() ([]byte, string, error) {
	b, err := json.Marshal(l)
	return b, "application/json", err
}

func newTranslator(t *testing.T) *translation.Translator {
	t.Helper()
	tr, err := translation.New(translation.Config{
		Store:           translation.NewMockStore(),
		DefaultLanguage: translation.LanguageRu,
		SupportedLangs:  []translation.Language{translation.LanguageRu, translation.LanguageKk},
	})
	if err != nil {
		t.Fatalf("new translator: %v", err)
	}
	return tr
}

func TestMiddleware_TranslatesSingleModel(t *testing.T) {
	tr := newTranslator(t)
	if err := tr.Save(context.Background(), translation.LanguageKk, &product{ID: "1", Title: "Атауы"}); err != nil {
		t.Fatalf("save: %v", err)
	}

	h := func(ctx context.Context, r *http.Request) rest.ResponseEncoder {
		return &product{ID: "1", Title: "Original"}
	}
	wrapped := translationrest.Middleware(nil, tr)(h)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Language", "kk")
	enc := wrapped(req.Context(), req)

	got := enc.(*product)
	if got.Title != "Атауы" {
		t.Fatalf("title = %q, want translated %q", got.Title, "Атауы")
	}
}

func TestMiddleware_DefaultLanguageIsNoop(t *testing.T) {
	tr := newTranslator(t)
	if err := tr.Save(context.Background(), translation.LanguageKk, &product{ID: "1", Title: "Атауы"}); err != nil {
		t.Fatalf("save: %v", err)
	}

	h := func(ctx context.Context, r *http.Request) rest.ResponseEncoder {
		return &product{ID: "1", Title: "Original"}
	}
	wrapped := translationrest.Middleware(nil, tr)(h)

	// No language header -> default (ru) -> original content preserved.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	enc := wrapped(req.Context(), req)

	if got := enc.(*product); got.Title != "Original" {
		t.Fatalf("title = %q, want untouched %q", got.Title, "Original")
	}
}

func TestMiddleware_TranslatesList(t *testing.T) {
	tr := newTranslator(t)
	ctx := context.Background()
	if err := tr.Save(ctx, translation.LanguageKk, &product{ID: "1", Title: "Бірінші"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := tr.Save(ctx, translation.LanguageKk, &product{ID: "2", Title: "Екінші"}); err != nil {
		t.Fatalf("save: %v", err)
	}

	h := func(ctx context.Context, r *http.Request) rest.ResponseEncoder {
		return productList{Items: []*product{{ID: "1", Title: "First"}, {ID: "2", Title: "Second"}}}
	}
	wrapped := translationrest.Middleware(nil, tr)(h)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Language", "kk")
	enc := wrapped(req.Context(), req)

	got := enc.(productList)
	if got.Items[0].Title != "Бірінші" || got.Items[1].Title != "Екінші" {
		t.Fatalf("titles = %q,%q, want translated", got.Items[0].Title, got.Items[1].Title)
	}
}

func TestMiddlewareWithLang_ReadsResolvedCode(t *testing.T) {
	tr := newTranslator(t)
	if err := tr.Save(context.Background(), translation.LanguageKk, &product{ID: "1", Title: "Атауы"}); err != nil {
		t.Fatalf("save: %v", err)
	}

	h := func(ctx context.Context, r *http.Request) rest.ResponseEncoder {
		return &product{ID: "1", Title: "Original"}
	}
	// Language comes from an upstream resolver reading the context (here a stub),
	// not from request headers.
	langOf := func(context.Context) string { return "kk" }
	wrapped := translationrest.MiddlewareWithLang(nil, tr, langOf)(h)

	req := httptest.NewRequest(http.MethodGet, "/", nil) // no language header
	enc := wrapped(req.Context(), req)

	if got := enc.(*product); got.Title != "Атауы" {
		t.Fatalf("title = %q, want translated %q", got.Title, "Атауы")
	}
}

func TestMiddleware_NonTranslatablePassthrough(t *testing.T) {
	tr := newTranslator(t)
	h := func(ctx context.Context, r *http.Request) rest.ResponseEncoder {
		return rest.JSON(map[string]string{"ok": "yes"})
	}
	wrapped := translationrest.Middleware(nil, tr)(h)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Language", "kk")
	enc := wrapped(req.Context(), req)
	if enc == nil {
		t.Fatal("expected passthrough encoder, got nil")
	}
}
