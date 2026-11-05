package buildinfo_test

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/assanoff/skit/buildinfo"
)

// Info prints as one line, logs as a group and encodes as JSON.
func ExampleInfo() {
	b := buildinfo.Info{
		Version:  "v1.2.3",
		Revision: "abc123",
		Time:     time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC),
		Modified: true,
		Go:       "go1.27.2",
	}
	fmt.Println(b)

	body, err := json.Marshal(b)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(body))

	h := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	})
	slog.New(h).InfoContext(context.Background(), "starting service", "build", b)
	// Output:
	// v1.2.3 (abc123, 2026-11-03T10:00:00Z, modified) go1.27.2
	// {"version":"v1.2.3","revision":"abc123","time":"2026-11-03T10:00:00Z","modified":true,"go":"go1.27.2"}
	// level=INFO msg="starting service" build.version=v1.2.3 build.revision=abc123 build.time=2026-11-03T10:00:00.000Z build.modified=true build.go=go1.27.2
}

// Read names the build where it is needed: the startup record, the Resource
// of the traces, GET /version.
func ExampleRead() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	log.InfoContext(context.Background(), "starting service", "build", buildinfo.Read())

	http.HandleFunc("GET /version", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.MarshalWrite(w, buildinfo.Read(), json.Deterministic(true))
	})
}
