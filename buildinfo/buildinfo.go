package buildinfo

import (
	"cmp"
	"log/slog"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

// version, when set at link time, wins over the version Go stamps from VCS:
//
//	go build -ldflags "-X github.com/assanoff/skit/buildinfo.version=v1.2.3"
var version string

// Info is what the binary knows about its build.
type Info struct {
	// Version is the link-time stamp, else the tag or pseudo-version Go
	// stamps from VCS, else "dev".
	Version string `json:"version"`
	// Revision is the VCS commit the binary was built from.
	Revision string `json:"revision,omitempty"`
	// Time is the time of that commit.
	Time time.Time `json:"time,omitzero"`
	// Modified reports a build from a tree with uncommitted changes.
	Modified bool `json:"modified,omitempty"`
	// Go is the Go version the binary was built with.
	Go string `json:"go"`
}

// Read returns the Info of the running binary.
func Read() Info {
	bi, _ := debug.ReadBuildInfo()
	return parse(version, bi)
}

// parse builds the Info of bi, nil when the binary carries no build
// information; a non-empty stamp is the version.
func parse(stamp string, bi *debug.BuildInfo) Info {
	info := Info{Version: cmp.Or(stamp, "dev"), Go: runtime.Version()}
	if bi == nil {
		return info
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		info.Version = cmp.Or(stamp, v)
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			info.Revision = s.Value
		case "vcs.time":
			info.Time, _ = time.Parse(time.RFC3339, s.Value)
		case "vcs.modified":
			info.Modified = s.Value == "true"
		}
	}
	return info
}

// String formats i for a person: "v1.2.3 (abc123, 2026-11-03T10:00:00Z,
// modified) go1.27.2", the part in parentheses only when VCS is known.
func (i Info) String() string {
	var vcs []string
	if i.Revision != "" {
		vcs = append(vcs, i.Revision)
	}
	if !i.Time.IsZero() {
		vcs = append(vcs, i.Time.Format(time.RFC3339))
	}
	if i.Modified {
		vcs = append(vcs, "modified")
	}
	if len(vcs) == 0 {
		return i.Version + " " + i.Go
	}
	return i.Version + " (" + strings.Join(vcs, ", ") + ") " + i.Go
}

// LogValue logs i as a group: version, then revision, time and modified when
// known, and go.
func (i Info) LogValue() slog.Value {
	attrs := []slog.Attr{slog.String("version", i.Version)}
	if i.Revision != "" {
		attrs = append(attrs, slog.String("revision", i.Revision))
	}
	if !i.Time.IsZero() {
		attrs = append(attrs, slog.Time("time", i.Time))
	}
	if i.Modified {
		attrs = append(attrs, slog.Bool("modified", true))
	}
	attrs = append(attrs, slog.String("go", i.Go))
	return slog.GroupValue(attrs...)
}
