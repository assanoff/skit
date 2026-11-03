package mid_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing/fstest"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/i18n"
	"github.com/assanoff/skit/rest"
	"github.com/assanoff/skit/rest/mid"
	"github.com/assanoff/skit/rest/router"
)

// show sends req to h and prints the status and the body.
func show(h http.Handler, req *http.Request) {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	fmt.Println(strings.TrimSpace(fmt.Sprintf("%d %s", w.Code, w.Body)))
}

// Chain is the standard application chain of a router. A panic becomes an
// Internal error, and with MaskInternal a 5xx reaches the client without its
// detail; a 4xx keeps it.
func ExampleChain() {
	r := router.New(mid.Chain(mid.Config{MaskInternal: true})...)
	r.HandleApp("GET /panic", func(context.Context, *http.Request) rest.ResponseEncoder {
		panic("nil map")
	})
	r.HandleApp("GET /db", func(context.Context, *http.Request) rest.ResponseEncoder {
		return errs.Newf(errs.Internal, `pq: relation "widgets" does not exist`)
	})
	r.HandleApp("GET /widgets/{id}", func(context.Context, *http.Request) rest.ResponseEncoder {
		return errs.Newf(errs.NotFound, "widget 7 not found")
	})

	for _, path := range []string{"/panic", "/db", "/widgets/7"} {
		show(r, httptest.NewRequest(http.MethodGet, path, nil))
	}
	// Output:
	// 500 {"code":"internal","detail":"internal server error"}
	// 500 {"code":"internal","detail":"internal server error"}
	// 404 {"code":"not_found","detail":"widget 7 not found"}
}

// LocalizeErrors translates an *errs.Error into the language of the request;
// i18n's middleware stores the language, LangFromContext reads it.
func ExampleLocalizeErrors() {
	tr, err := i18n.New(fstest.MapFS{
		"locales/en.json": {Data: []byte(`{"widget_not_found": "widget {{.id}} not found"}`)},
		"locales/ru.json": {Data: []byte(`{"widget_not_found": "виджет {{.id}} не найден"}`)},
	}, i18n.Config{DefaultLang: "en", Files: []string{"locales/en.json", "locales/ru.json"}})
	if err != nil {
		fmt.Println(err)
		return
	}

	r := router.New(mid.LocalizeErrors(tr, i18n.LangFromContext))
	r.Use(tr.Middleware())
	r.HandleApp("GET /widgets/{id}", func(_ context.Context, r *http.Request) rest.ResponseEncoder {
		id := rest.Param(r, "id")
		return errs.Newf(errs.NotFound, "widget %s not found", id).
			WithMessageID("widget_not_found").
			WithArgs(map[string]any{"id": id})
	})

	req := httptest.NewRequest(http.MethodGet, "/widgets/7", nil)
	req.Header.Set("Accept-Language", "ru-RU")
	show(r, req)
	// Output:
	// 404 {"code":"not_found","detail":"виджет 7 не найден"}
}

// ETag tags a successful response by its body and answers a matching
// If-None-Match with 304; CacheControl lets the client keep it.
func ExampleETag() {
	r := router.New()
	r.HandleApp("GET /widgets/{id}", func(_ context.Context, r *http.Request) rest.ResponseEncoder {
		return rest.JSON(map[string]string{"id": rest.Param(r, "id")})
	}, mid.ETag(), mid.CacheControl(60))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/widgets/42", nil))
	tag := w.Header().Get("ETag")
	fmt.Println(w.Code, tag, w.Header().Get("Cache-Control"))

	req := httptest.NewRequest(http.MethodGet, "/widgets/42", nil)
	req.Header.Set("If-None-Match", tag)
	show(r, req)
	// Output:
	// 200 "8aa5b6751d68bd72901cea96bbbddca8" public, max-age=60
	// 304
}
