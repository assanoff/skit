// Package buildinfo reports the build of the running binary: its version, the
// VCS revision and time it was built from, and the Go version.
//
// go build in a git checkout stamps the version of the main module from VCS:
// the tag (v1.2.3) or a pseudo-version, +dirty for a tree with uncommitted
// changes, plus the revision and its time. A build without VCS information —
// go run, a Docker build without .git, -buildvcs=false — gets "dev". To set
// the version there, stamp it at link time; the stamp wins over VCS:
//
//	go build -ldflags "-X github.com/assanoff/skit/buildinfo.version=$(VERSION)"
//
// # Usage
//
// Read where the build is named; it costs a parse of the build information,
// so a caller does not thread the result through its config:
//
//	b := buildinfo.Read()
//	log := logger.New(os.Stdout, logger.Config{Service: "orders", Version: b.Version})
//	log.InfoContext(ctx, "starting service", "build", b) // version, revision, time, modified, go
//
// The same version names the build in the Resource of otel/setup
// (Config.Version) and in the events of provider/sentry (Config.Release);
// the whole Info is the body of GET /version on the status server
// (rest.JSON(buildinfo.Read()), or Config.Version of debugsrv).
//
// # Info
//
//   - Version: the link-time stamp, else the VCS version, else "dev".
//   - Revision, Time, Modified: from VCS, empty without it.
//   - Go: the Go version of the build.
//
// Info prints as "v1.2.3 (abc123, 2026-11-03T10:00:00Z, modified) go1.27.2"
// (String), logs as a group (LogValue) and encodes as JSON.
package buildinfo
