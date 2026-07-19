// Command skit scaffolds and manages skit-based services.
//
// It is a CLI built on github.com/jessevdk/go-flags with three subcommands: new
// (scaffold a service), add (scaffold a module into an existing service) and
// version (print the CLI version, overridable at build time with
// -ldflags "-X main.version=...").
//
// # skit new
//
// Creates a new service module. Without --template it renders an embedded
// minimal starter; with --template it delegates to the gonew tool (an installed
// gonew binary, or `go run golang.org/x/tools/cmd/gonew@latest` as a fallback).
// The target directory must not already exist with content, so an existing
// project is never overwritten.
//
// Usage:
//
//	skit new <module-path> [flags]
//
// The positional module-path is required (e.g. github.com/you/svc). Flags:
//
//	--dir          target directory (default: last element of the module path)
//	--template     gonew template module; delegates to gonew when set
//	--sdk-version  skit version for go.mod (default "latest"; "latest"
//	               omits the require — run go mod tidy)
//
// # skit add rest
//
// Scaffolds a per-entity REST CRUD module into the service in the current
// directory, following the skit conventions: a domain Core that depends
// only on a Store interface (core/<name>/), a Postgres implementation of that
// Store (core/<name>/<name>db/), and a REST transport with create/list/get/
// update/delete handlers (api/<name>/). The list endpoint ships the full
// skit listing set out of the box: offset pagination (?page/?rows ->
// query.Result, the {error_code, data:{items, pagination}} envelope), keyset
// cursor pagination (GET /<plural>/cursor -> query.CursorResult), a QueryFilter (?name/?description,
// honored by Query and Count), and allowlisted ORDER BY (?order_by, via the
// generated <name>db/order.go). It also generates a store-level integration test
// (<name>db/listing_test.go) that exercises the listing set — cursor, offset,
// filter, ordering — against a real Postgres via the SDK dbtest helper
// (testcontainers; skipped under `go test -short`). The module path is read from
// go.mod (override with --module). Existing files are never overwritten. The entity name may be
// kebab, snake or camelCase ("order-line", "orderLine") — it is normalized to a
// package name and an exported Go type. The command prints the migration (table +
// keyset index) and wiring a developer must still add by hand (both app-specific).
//
// Usage:
//
//	skit add rest <name> [flags]
//
// Flags:
//
//	--dir     service root containing go.mod (default: current directory)
//	--module  module path (default: read from go.mod)
//	--plural  route/table plural (default: <name>+"s")
//
// # skit add grpc
//
// Scaffolds a gRPC module for one entity: a protobuf-editions .proto contract
// (proto/<name>/v1/) and a thin handler (internal/app/handlers/<name>grpc/) that
// adapts the generated service to the entity's Core, returning *errs.Error so the
// server interceptor maps it to a gRPC status. It adapts core/<name>, so run
// `skit add rest <name>` first. The proto uses edition 2023 with the Opaque
// Go API; the command prints the codegen (make proto / buf generate) and server
// registration to run by hand. Existing files are never overwritten.
//
// Usage:
//
//	skit add grpc <name> [flags]
//
// Flags:
//
//	--dir     service root containing go.mod (default: current directory)
//	--module  module path (default: read from go.mod)
//	--plural  List RPC / list-field plural (default: <name>+"s")
//
// # Examples
//
//	skit new github.com/you/svc                      # embedded starter
//	skit new github.com/you/svc --dir ./svc
//	skit new github.com/you/svc --template github.com/some/template
//	skit add rest widget                             # core + store + REST
//	skit add rest category --plural categories
//	skit add grpc widget                             # .proto + gRPC handler
//	skit version
package main
