# Quick start

Go from nothing to a running skit service with REST (and optionally gRPC)
endpoints, using only the `skit` CLI to generate the boilerplate.

## 0. Get the CLI

The CLI lives in `cmd/skit`. You can either run it from a checkout of this repo
or build it into `bin/`:

```bash
# from a checkout of the SDK
go run ./cmd/skit version

# or build the binary (lands in bin/, which is gitignored)
make build-cli
./bin/skit version
```

> Never run a bare `go build ./cmd/skit` — it drops a `skit` binary at the repo
> root. Use `make build-cli` (→ `bin/skit`) or `go run ./cmd/skit`.

The examples below use `skit` as the command; substitute `go run ./cmd/skit` if
you have not built the binary.

## 1. Scaffold a new service

```bash
skit new github.com/you/svc          # embedded minimal starter
```

This writes a starter into `./svc/` (the last element of the module path; override
with `--dir`):

```
svc/
  go.mod          # module github.com/you/svc, requires github.com/assanoff/skit
  main.go         # worker.Group running an http.Server with /healthz + /hello
  README.md
```

Other options:

```bash
skit new github.com/you/svc --dir ./service          # custom target dir
skit new github.com/you/svc --template github.com/some/tmpl   # delegate to gonew
```

The target directory must be empty — an existing project is never overwritten.

## 2. Run it

```bash
cd svc
go mod tidy        # resolve github.com/assanoff/skit
go run .

# in another shell
curl localhost:8080/hello       # hello from svc
curl localhost:8080/healthz     # liveness probe
```

The starter `main.go` wires a `logger`, a `router`, and a `worker.Group`
supervising the HTTP server with graceful shutdown on SIGINT/SIGTERM.

## 3. Add a REST CRUD module (boilerplate)

Run this from the service root (the directory holding `go.mod`). The entity name
may be kebab, snake, or camelCase (`order-line`, `order_line`, `orderLine`) — it
is normalized to a package name and an exported Go type.

```bash
skit add rest widget
skit add rest category --plural categories   # override the route/table plural
```

This generates, following the skit conventions:

```
core/widget/widget.go          # domain Core, depends only on a Store interface
core/widget/model.go           # domain model
core/widget/widgetdb/widgetdb.go   # Postgres implementation of Store
core/widget/widgetdb/model.go      # rowDB + scanning
core/widget/widgetdb/order.go      # allowlisted ORDER BY columns
core/widget/widgetdb/listing_test.go   # integration test (testcontainers)
api/widget/widget.go           # REST handlers: create/list/get/update/delete
api/widget/model.go            # App DTOs + transforms
```

The list endpoint ships the full skit listing set out of the box: offset
pagination (`?page`/`?rows`), keyset cursor pagination (`GET /widgets/cursor`), a
query filter (`?name`/`?description`), and allowlisted `?order_by`.

The CLI never overwrites existing files. After generating, it prints two
app-specific steps you must do by hand:

**a. Add a migration** for the table (the composite index backs cursor listing):

```sql
-- internal/migrations/NNNN_widgets.sql
CREATE TABLE widgets (
    id          UUID PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX widgets_created_at_id_desc_idx ON widgets (created_at DESC, id DESC);
```

**b. Wire it** where you build the router:

```go
store := widgetdb.NewStore(log, db)
core  := widget.NewCore(log, store)
widgetapi.New(core).Routes(r.HandleApp)   // import alias: widgetapi "github.com/you/svc/api/widget"
```

Then:

```bash
go mod tidy && go build ./...
go test ./core/widget/...     # listing test; needs Docker, skipped under -short
```

## 4. Add a gRPC module (optional)

Adapts an existing `core/<name>`, so run `skit add rest <name>` first.

```bash
skit add grpc widget
```

This generates a protobuf-editions contract and a thin handler:

```
proto/widget/v1/widget.proto                    # edition 2023, Opaque Go API
internal/app/handlers/widgetgrpc/widgetgrpc.go  # adapts widget.Core
```

Then generate the code and register the service (the CLI prints these):

```bash
make proto      # buf lint + generate (commits to gen/widget/v1/)
```

```go
gs.Install(widgetgrpc.New(core))   // where you build the gRPC server
go build ./...
```

## Command reference

| Command | What it does |
|---|---|
| `skit new <module>` | Scaffold a new service module |
| `skit new <module> --template <tmpl>` | Scaffold via a gonew template |
| `skit add rest <name>` | Core + Postgres store + REST transport for one entity |
| `skit add grpc <name>` | `.proto` contract + gRPC handler adapting one entity's Core |
| `skit version` | Print the CLI version |

Common flags for `add`: `--dir` (service root, default `.`), `--module` (default
read from `go.mod`), `--plural` (route/table plural, default `<name>s`).

For a full application wiring every package end-to-end, see
[`skit-x`](https://github.com/assanoff/skit-x).
