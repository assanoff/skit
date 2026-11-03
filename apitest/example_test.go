package apitest_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/apitest"
)

// A test drives the application handler over HTTP. New starts the server and
// closes it when the test ends; a helper fails the test itself on a transport
// error or an unmet expectation, so the test reads as a script.
func Example() {
	mux := http.NewServeMux() // the router of the application
	mux.HandleFunc("POST /widgets", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"id":"w-1","name":"gadget"}`)
	})
	mux.HandleFunc("GET /widgets", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `[{"id":"w-1","name":"gadget"}]`)
	})

	var t *testing.T // the *testing.T of the test, e.g. TestWidgets
	is := is.New(t)
	srv := apitest.New(t, mux)

	created := srv.PostJSON("/widgets", `{"name":"gadget"}`).
		ExpectStatus(http.StatusCreated).
		JSON()
	id, _ := created["id"].(string)

	srv.Get("/widgets").ExpectStatus(http.StatusUnauthorized)
	list := srv.Get("/widgets", apitest.WithBearer("t-1")).
		ExpectStatus(http.StatusOK).
		JSONArray()
	is.Equal(len(list), 1)
	is.Equal(list[0]["id"], id)
}
