package apitest_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/apitest"
)

// echo answers with the request it got as a JSON object, and with a JSON array
// of it on /list; DELETE answers 204.
func echo() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		body, _ := io.ReadAll(r.Body)
		got := map[string]any{
			"method":        r.Method,
			"path":          r.URL.Path,
			"body":          string(body),
			"content_type":  r.Header.Get("Content-Type"),
			"authorization": r.Header.Get("Authorization"),
			"tenant":        r.Header.Get("X-Tenant"),
		}
		if r.URL.Path == "/list" {
			_ = json.NewEncoder(w).Encode([]any{got})
			return
		}
		_ = json.NewEncoder(w).Encode(got)
	})
}

func TestRequests(t *testing.T) {
	srv := apitest.New(t, echo())

	tests := []struct {
		name   string
		resp   func() *apitest.Response
		method string
		body   string
		ctype  string
	}{
		{
			name:   "get",
			resp:   func() *apitest.Response { return srv.Get("/widgets") },
			method: http.MethodGet,
		},
		{
			name: "post",
			resp: func() *apitest.Response {
				return srv.PostJSON("/widgets", `{"name":"gadget"}`)
			},
			method: http.MethodPost,
			body:   `{"name":"gadget"}`,
			ctype:  "application/json",
		},
		{
			name: "put",
			resp: func() *apitest.Response {
				return srv.PutJSON("/widgets", `{"name":"gear"}`)
			},
			method: http.MethodPut,
			body:   `{"name":"gear"}`,
			ctype:  "application/json",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)
			got := tt.resp().ExpectStatus(http.StatusOK).JSON()
			is.Equal(got["method"], tt.method)
			is.Equal(got["path"], "/widgets")
			is.Equal(got["body"], tt.body)
			is.Equal(got["content_type"], tt.ctype) // JSON only with a body
		})
	}
}

func TestDelete(t *testing.T) {
	is := is.New(t)
	srv := apitest.New(t, echo())

	resp := srv.Delete("/widgets/w-1").ExpectStatus(http.StatusNoContent)
	is.Equal(len(resp.Body), 0)
}

func TestOptions(t *testing.T) {
	is := is.New(t)
	srv := apitest.New(t, echo())

	got := srv.Get("/widgets",
		apitest.WithBearer("t-1"),
		apitest.WithHeader("X-Tenant", "acme"),
	).JSON()
	is.Equal(got["authorization"], "Bearer t-1")
	is.Equal(got["tenant"], "acme")
}

func TestDecode(t *testing.T) {
	is := is.New(t)
	srv := apitest.New(t, echo())

	var got struct {
		Method string `json:"method"`
		Path   string `json:"path"`
	}
	srv.Get("/widgets").Decode(&got)
	is.Equal(got.Method, http.MethodGet)
	is.Equal(got.Path, "/widgets")

	list := srv.Get("/list").JSONArray()
	is.Equal(len(list), 1)
	is.Equal(list[0]["path"], "/list")
}
