package buildinfo

import (
	"bytes"
	"encoding/json/v2"
	"log/slog"
	"runtime"
	"runtime/debug"
	"testing"
	"time"

	"github.com/matryer/is"
)

func vcsBuild(version string) *debug.BuildInfo {
	return &debug.BuildInfo{
		Main: debug.Module{Path: "example.com/orders", Version: version},
		Settings: []debug.BuildSetting{
			{Key: "vcs", Value: "git"},
			{Key: "vcs.revision", Value: "abc123"},
			{Key: "vcs.time", Value: "2026-11-03T10:00:00Z"},
			{Key: "vcs.modified", Value: "true"},
		},
	}
}

func TestParse(t *testing.T) {
	is := is.New(t)
	at := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)

	got := parse("", vcsBuild("v1.2.3"))
	is.Equal(got, Info{
		Version: "v1.2.3", Revision: "abc123", Time: at, Modified: true, Go: runtime.Version(),
	})
	is.Equal(parse("v9.0.0", vcsBuild("v1.2.3")).Version, "v9.0.0") // the stamp wins
	is.Equal(parse("v9.0.0", nil).Version, "v9.0.0")                // even without build info

	is.Equal(parse("", vcsBuild("(devel)")).Version, "dev") // go run
	none := parse("", nil)
	is.Equal(none, Info{Version: "dev", Go: runtime.Version()}) // no build info at all
}

func TestRead(t *testing.T) {
	is := is.New(t)
	got := Read()
	is.True(got.Version != "")
	is.Equal(got.Go, runtime.Version())
}

func TestString(t *testing.T) {
	is := is.New(t)
	v := parse("", vcsBuild("v1.2.3"))
	is.Equal(v.String(), "v1.2.3 (abc123, 2026-11-03T10:00:00Z, modified) "+runtime.Version())
	is.Equal(Info{Version: "dev", Go: "go1.27.2"}.String(), "dev go1.27.2") // no VCS
}

func TestLogValue(t *testing.T) {
	is := is.New(t)
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	log.InfoContext(t.Context(), "starting service", "build", parse("", vcsBuild("v1.2.3")))

	var rec struct {
		Build map[string]any `json:"build"`
	}
	is.NoErr(json.Unmarshal(buf.Bytes(), &rec))
	is.Equal(rec.Build["version"], "v1.2.3")
	is.Equal(rec.Build["revision"], "abc123")
	is.Equal(rec.Build["time"], "2026-11-03T10:00:00Z")
	is.Equal(rec.Build["modified"], true)
	is.Equal(rec.Build["go"], runtime.Version())
}

func TestJSON(t *testing.T) {
	is := is.New(t)
	b, err := json.Marshal(Info{Version: "dev", Go: "go1.27.2"})
	is.NoErr(err)
	is.Equal(string(b), `{"version":"dev","go":"go1.27.2"}`) // no VCS fields without VCS
}
