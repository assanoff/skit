package httplog_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/assanoff/skit/httplog"
	"github.com/assanoff/skit/rest/router"
)

// stable drops what changes from run to run, the time and the duration, so
// the example prints the same record every time.
func stable(groups []string, a slog.Attr) slog.Attr {
	if len(groups) > 0 {
		return a
	}
	switch a.Key {
	case slog.TimeKey, httplog.OTELResponseDuration:
		return slog.Attr{}
	case slog.MessageKey:
		msg, _, _ := strings.Cut(a.Value.String(), " (")
		return slog.String(a.Key, msg)
	}
	return a
}

// RequestLogger writes one record per request through the logger of the
// service. A handler adds its attributes with SetAttrs and its error with
// SetError; the status sets the level.
func Example() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{ReplaceAttr: stable}))
	access := httplog.RequestLogger(log, &httplog.Options{
		Schema:        httplog.SchemaOTEL.Concise(true),
		RecoverPanics: true,
	})

	r := router.New()
	api := r.With(access) // the group of business routes, after middleware.Trace
	api.HandleFunc("GET /widgets/{id}", func(w http.ResponseWriter, r *http.Request) {
		httplog.SetAttrs(r.Context(), slog.String("widget.id", r.PathValue("id")))
		w.WriteHeader(http.StatusOK)
	})
	api.HandleFunc("GET /report", func(w http.ResponseWriter, r *http.Request) {
		err := httplog.SetError(r.Context(), errors.New("clickhouse: timeout"))
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
	})

	for _, path := range []string{"/widgets/42?token=secret", "/report"} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}
	// Output:
	// {"level":"INFO","msg":"GET /widgets/42 => HTTP 200","http.request.method":"GET","client.address":"192.0.2.1","http.response.status_code":200,"http.route":"/widgets/{id}","widget.id":"42"}
	// {"level":"ERROR","msg":"GET /report => HTTP 503","http.request.method":"GET","client.address":"192.0.2.1","http.response.status_code":503,"http.route":"/report","err":"clickhouse: timeout"}
}

// ReplaceAttr renames the base fields of every record of the service, not
// only the access log, to the names of a schema, here ECS.
func ExampleSchema_ReplaceAttr() {
	ecs := func(groups []string, a slog.Attr) slog.Attr {
		if a.Key == slog.TimeKey {
			return slog.Attr{} // a stable output
		}
		return httplog.SchemaECS.ReplaceAttr(groups, a)
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{ReplaceAttr: ecs}))

	log.InfoContext(context.Background(), "server started", "port", 8080)
	// Output:
	// {"log.level":"INFO","message":"server started","port":8080}
}

// CURL turns a request into a curl command, e.g. in LogExtraAttrs to replay a
// request the server rejected.
func ExampleCURL() {
	req := httptest.NewRequest(http.MethodPost, "/v1/widgets", nil)
	req.Header.Set("Content-Type", "application/json")

	fmt.Println(httplog.CURL(req, `{"name":"gadget"}`))
	// Output:
	// curl 'http://example.com/v1/widgets' --data-raw '{"name":"gadget"}' -H 'Content-Type: application/json'
}
