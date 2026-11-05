package rest_test

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/page"
	"github.com/assanoff/skit/rest"
	"github.com/assanoff/skit/validate"
)

type widget struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// serve sends a request to h and prints the status and the body.
func serve(h http.Handler, method, target, body string) {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, target, strings.NewReader(body)))
	fmt.Println(strings.TrimSpace(fmt.Sprintf("%d %s", w.Code, w.Body)))
}

// A typed handler returns its response, a value or an *errs.Error, and
// HandlerFunc.ServeHTTP writes it: the status, the content type and the body.
// A HandlerFunc is an http.Handler, so it registers on any router.
func Example() {
	widgets := map[string]widget{"42": {ID: "42", Name: "gadget"}}
	get := func(_ context.Context, r *http.Request) rest.ResponseEncoder {
		id := rest.Param(r, "id")
		w, ok := widgets[id]
		if !ok {
			return errs.Newf(errs.NotFound, "widget %s not found", id)
		}
		return rest.JSON(w)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /widgets/{id}", rest.HandlerFunc(get))

	serve(mux, http.MethodGet, "/widgets/42", "")
	serve(mux, http.MethodGet, "/widgets/7", "")
	// Output:
	// 200 {"id":"42","name":"gadget"}
	// 404 {"code":"not_found","detail":"widget 7 not found"}
}

// createWidget is a request model: it decodes its own bytes and validates
// itself.
type createWidget struct {
	Name string `json:"name" validate:"required"`
}

func (c *createWidget) Decode(data []byte) error {
	return json.Unmarshal(data, c)
}

func (c *createWidget) Validate() error { return validate.Check(c) }

// Decode reads the body into the model and runs its Validate: bad input is an
// InvalidArgument, a 400. JSONStatus answers with a status of choice.
func ExampleDecode() {
	create := func(_ context.Context, r *http.Request) rest.ResponseEncoder {
		var in createWidget
		if err := rest.Decode(r, &in); err != nil {
			return errs.From(err)
		}
		return rest.JSONStatus(widget{ID: "43", Name: in.Name}, http.StatusCreated)
	}

	h := rest.HandlerFunc(create)
	serve(h, http.MethodPost, "/widgets", `{"name":"gizmo"}`)
	serve(h, http.MethodPost, "/widgets", `{}`)
	serve(h, http.MethodPost, "/widgets", `{"name":`)
	// Output:
	// 201 {"id":"43","name":"gizmo"}
	// 400 {"code":"invalid_argument","title":"data validation error","detail":"validation failed","fields":[{"field":"name","error":"required"}]}
	// 400 {"code":"invalid_argument","detail":"decode request body: jsontext: unexpected EOF within \"/name\" after offset 8"}
}

// The envelopes give every endpoint one shape, an error_code and the data: a
// page of a list, one object, a cursor page.
func ExampleNewResult() {
	items := []widget{{ID: "3", Name: "gear"}, {ID: "4", Name: "bolt"}}

	for _, resp := range []rest.ResponseEncoder{
		rest.NewResult(items, 5, page.MustParse("2", "2")),
		rest.NewResultItem(items[0]),
		rest.NewCursorResult(items, "eyJpZCI6NH0", ""),
	} {
		body, _, _ := resp.Encode()
		fmt.Println(string(body))
	}
	// Output:
	// {"error_code":"ok","data":{"items":[{"id":"3","name":"gear"},{"id":"4","name":"bolt"}],"pagination":{"total_pages":3,"current_page":2,"limit":2,"total_items":5}}}
	// {"error_code":"ok","data":{"id":"3","name":"gear"}}
	// {"error_code":"ok","data":{"items":[{"id":"3","name":"gear"},{"id":"4","name":"bolt"}],"next":"eyJpZCI6NH0"}}
}

// ChainMiddleware wraps a handler in application middleware, the first
// outermost; Chain seals several into one MidFunc that travels as a value.
func ExampleChainMiddleware() {
	step := func(name string) rest.MidFunc {
		return func(next rest.HandlerFunc) rest.HandlerFunc {
			return func(ctx context.Context, r *http.Request) rest.ResponseEncoder {
				fmt.Println("enter", name)
				defer fmt.Println("leave", name)
				return next(ctx, r)
			}
		}
	}
	remove := func(context.Context, *http.Request) rest.ResponseEncoder {
		fmt.Println("handler")
		return nil // 204 No Content
	}

	inner := rest.Chain(step("tx"), step("log"))
	h := rest.ChainMiddleware(remove, step("auth"), inner)
	serve(h, http.MethodDelete, "/widgets/42", "")
	// Output:
	// enter auth
	// enter tx
	// enter log
	// handler
	// leave log
	// leave tx
	// leave auth
	// 204
}
