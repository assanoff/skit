# skit

skit ([S]ervice [KIT]) Go SDK (software development kit) for building production web services that speak **REST and/or gRPC**, backed by Postgres, with first-class observability (slog + OpenTelemetry traces + Prometheus metrics), reliable background workers, and a transactional outbox.

This is NOT a framework.
It is just a building block for production-ready applications in Go.

It distills patterns from several reference services into a single, importable module
(`github.com/assanoff/skit`). A separate repo,
[`skit-x`](https://github.com/assanoff/skit-x), is the runnable showcase: a full
CRUD app that imports this SDK from GitHub and exercises every package end-to-end (and is where the
`skit` CLI is tried out).

## Layout

| Package | Purpose |
|---|---|
| `errs` | Error type with stable codes, REST/gRPC mapping, validation, secret sanitization |
| `logger` | `slog`-based logger: `trace_id` injection, level fan-out (stdout + Sentry), additive per-level side-effect hooks (`Events`, e.g. alert on error), caller-skip variants, access log |
| `otel` | Tracing bootstrap, span helpers, `GetTraceID`, route-excluding sampler |
| `dbx` | `sqlx`/`pgx` helpers: named queries, `WithinTran`, bulk insert/upsert/update, retries |
| `dim` | Slim dependency-injection: lazy `Provider[T]`, `Once`/`OnceWithName`, `NewResource` with init/cleanup logging |
| `closer` | Graceful-shutdown registry: process-global default (`Add`/`CloseSync`) + instances (`New`/`NewWithWait`), LIFO |
| `config` | 12-factor config via go-flags: one struct → CLI flags + env vars + `--help`, dotenv for local, subcommand dispatch |
| `rest` (+ `router`/`mid`) | Typed HTTP handlers over stdlib `ServeMux` + `routegroup`; routing (`router`) and typed application middleware (`rest/mid`: error localize/mask, panics, observability, cache, ETag) |
| `middleware` | net/http **server** middleware (`func(http.Handler) http.Handler`): panic recovery, trace-context, access log, timeout, body-size limit (cf. `httpmw` for the **client** side) |
| `httpserver` | `http.Handler`-on-a-listener as a `worker.Runnable`; the shared server core for REST, the gRPC-gateway and `debugsrv`; idle/header timeouts, `net/http` errors to your logger, stuck connections closed on shutdown |
| `order`, `page`, `query` | Listing primitives shared by the domain and both transports: allowlisted sort (`order`), validated offset/cursor paging input (`page`), paginated result envelope (`query`) |
| `grpcserver` | gRPC server as a `worker.Runnable`: recovery/trace/logging/metrics interceptors, `errs`→status mapping, health, reflection, transport tuning knobs |
| `worker` | Unified worker abstraction: `Runnable`, `Group`, `Loop` (fixed) / `NewPacedLoop` (adaptive: drain-when-busy, idle-backoff), `Pool`, `Processor[T]`, `Backoff` |
| `queue` | Durable work queue (`Schedule`/`Claim`/`MarkDone`/`MarkFailed`): Postgres (`FOR UPDATE SKIP LOCKED`) + in-memory, plugs into `worker.Processor` as a `Source`/`Sink` |
| `poller` | Typed cache of a periodically-refreshed value (`Poller[T]`), a `worker.Runnable` |
| `broker` | Transport-agnostic `Publisher`/`Consumer` abstraction; CloudEvents v1.0 envelope; `broker/rabbitmq` implementation (confirm-mode publisher, `worker.Runnable` consumer) |
| `outbox` | Transactional outbox: the domain emits a typed event through a tx-bound `Publisher` (route resolved from a `Registry`, transport-neutral `Topic`/`Key`) inside `WithinTran` — atomic domain-write + event, no SQL/transport leaking into the domain. `Relay`/`Sweeper`/`Cleaner` workers (built on `worker.Processor`/`Loop`) deliver via any `broker.Publisher` |
| `eventbus` | In-process synchronous event bus (`Bus`) for decoupling domains without import cycles: `Call` (abort on first error) / `Publish` (run all, join errors). For atomic/durable events use `outbox` instead |
| `i18n` | go-i18n wrapper: catalogs from `fs.FS`, Accept-Language matching + middleware, localizes `*errs.Error` by `MessageID`/`Code` with `Args` |
| `auth` | `Principal` in context, credential extraction, pluggable `Verifier`, built-in JWT (HMAC/RSA/EC + JWKS via Keyfunc), `Authenticate`/`Optional`/`RequireRole` middleware |
| `metrics` | Prometheus registry + HTTP middleware; extensible model — one shared registry, each package owns its collectors, `Register` (register-or-get) makes it conflict-free so SDK and app metrics coexist |
| `health` | Liveness/readiness handlers for Kubernetes probes |
| `debugsrv` | `net/http/pprof` + optional metrics/health as one `http.Handler` (`Handler`); run it standalone on a separate port (`New`, a `worker.Runnable` that is also an `http.Handler`), attach it to the app router at `Paths`, or mount pprof alone (`Pprof`) |
| `migrate` | goose wrapper applying SQL migrations from an `fs.FS` via the Provider API (no goose global state) |
| `httpmw` | Outbound HTTP-client middleware: `RetryTransport` retries 429/503 with `worker.Backoff` + RFC 7231 `Retry-After` |
| `dbtest` | testcontainers Postgres for integration tests: start, migrate, connect, auto-teardown |
| `apitest` | Stdlib-only HTTP test helpers: JSON requests, status/body assertions against an `httptest.Server` |
| `safetick` | Panic recovery for worker ticks and consumer callbacks |
| `httplog` | Access logger with OpenTelemetry trace correlation and ECS-HTTP formatting |
| `httpclient` | Typed JSON HTTP client with OAuth2 (client-credentials) support and pluggable request middleware |
| `retry` | Generic retry with pluggable `Backoff` (exponential + jitter) and terminal-error short-circuit; the backoff shared by `httpmw`/`worker` |
| `cron` | Cron scheduler (`robfig/cron`) as a `worker.Runnable`: spec-validated jobs, per-firing panic recovery, graceful drain |
| `lock` | Distributed mutual exclusion behind a common `Locker`: Postgres advisory locks and single-instance Redis locks |
| `chx` | Optional ClickHouse helper (connect + goose migrations); pulls the ClickHouse driver only when imported |
| `grpcgateway` | grpc-gateway REST↔gRPC transcoding as a `worker.Runnable`, sharing the app's server core |
| `grpctest` | In-process gRPC test harness over `bufconn` running the real `grpcserver` interceptor chain |
| `provider` | Ready-made `dim` factories for common dependencies (Postgres, Redis, RabbitMQ, tracer, Sentry, translator) |
| `app` | Opinionated application entrypoint: wires config → deps → `worker.Group` and drives graceful shutdown via `closer` |
| `to` | Small response-encoding helpers (`to.JSON`, JSON:API) — a convenience seam over `rest` |
| `cmd/skit` | Scaffolding CLI: `skit new <module>` (embedded starter, `--full` service layout, or gonew passthrough with `--template`), plus per-model generators via `skit add …` |

The `skit add` generators scaffold per-model code: `rest`, `rest-test`, `grpc`, `grpc-test`,
`consumer`, `worker`, `migration`, `event`, `cron`, and `http-client`. See
[quick-start.md](quick-start.md) for an end-to-end walkthrough.

## Installation

Your code always imports the same path — `github.com/assanoff/skit/...` — regardless of how
the source is supplied, because Go resolves packages by import path. So both recipes below produce
**identical runtime behavior**; they differ only in versioning and workflow. The key rule for the
local recipe: keep the module path `github.com/assanoff/skit` and wire it in with `replace`
or a workspace — do **not** rename the import paths (that forks the SDK).

### External dependency (recommended)

Versioned, reproducible, `go.sum`-verified. This is what `skit new` generates.

```bash
go get github.com/assanoff/skit@v0.6.0   # or @latest
```

```go
import (
    "github.com/assanoff/skit/rest/router"
    "github.com/assanoff/skit/worker"
)
```

Update with `go get github.com/assanoff/skit@latest`. Pin a different version anytime by
re-running `go get …@vX.Y.Z`.

### Internal dependency (local source on disk)

Use this to develop your service and the SDK side by side, building against a local checkout. Two
equivalent ways — pick one:

**A. `replace` directive** (per-module, committed to your app's `go.mod`):

```bash
# in your service module
go get github.com/assanoff/skit@v0.6.0          # keeps a sane version + go.sum entry
go mod edit -replace github.com/assanoff/skit=../skit
go mod tidy
```

```
// resulting go.mod
require github.com/assanoff/skit v0.6.0
replace github.com/assanoff/skit => ../skit
```

**B. Go workspace** (`go.work`, not committed — local to your machine):

```bash
cd /path/to/workspace        # parent dir holding both checkouts
go work init ./your-service ./skit
```

Either way the import paths stay `github.com/assanoff/skit/...` and the build uses the
on-disk source. This is exactly how
[`skit-x`](https://github.com/assanoff/skit-x) builds against the SDK during local
development (`replace github.com/assanoff/skit => ../skit`).

Caveats for the local recipe:

- It builds against **whatever is on disk** — not version-pinned and not `go.sum`-verified for the
  replaced module; the path must exist everywhere you build (CI, teammates).
- `replace`/`go.work` are **not transitive and not published** — they apply only to your own build.
  Fine for an application (the main module); for a library you publish, ship the external dependency
  and drop the replace before tagging.
- Recommended flow: keep the external `require` pinned, add `replace` only while iterating on the
  SDK, then remove it.

## i18n & auth

- `i18n.Translator` loads JSON catalogs from an embed.FS, resolves the request
  language from `Accept-Language`, and localizes error responses: an app
  middleware translates any `*errs.Error` by its `MessageID` (or `Code`) with
  `Args` as template data. The example localizes widget errors to en/ru.
- `auth` verifies bearer tokens via a pluggable `Verifier` (built-in JWT covers
  HMAC, RSA/EC PEM, and JWKS through a custom Keyfunc) and enforces RBAC with
  `Authenticate` + `RequireRole`. The example protects widget writes behind a
  `widget:write` role while keeping reads public.

## Scaffolding

```bash
go run ./cmd/skit new github.com/you/svc          # embedded starter
go run ./cmd/skit new github.com/you/svc --template github.com/some/tmpl  # via gonew
```

See [quick-start.md](quick-start.md) for an end-to-end walkthrough: scaffolding a
service, running it, and generating REST/gRPC modules with the `skit` CLI.

## Messaging & transactional outbox

`broker` + `outbox` give reliable, exactly-the-domain-write event publishing
with a domain layer that knows nothing about SQL or the transport:

- The domain emits a **plain typed value** through a tx-bound `outbox.Publisher`:

  ```go
  err := outbox.WithinTran(ctx, log, db, store, reg, func(tx *sqlx.Tx, pub outbox.Publisher) error {
      if err := repo.WithTx(tx).Create(ctx, w); err != nil { // domain write
          return err
      }
      return pub.Publish(ctx, widget.Created{ID: w.ID.String()}) // domain event
  })
  ```

  The event and the domain write commit in ONE transaction — an event is
  persisted iff its domain write commits. The domain names no exchange, topic,
  or routing key; it doesn't even hold a transaction.

- Routing is a **wiring concern**. A `Registry`, populated once at startup, maps
  each event's Go type to a transport-neutral `Topic` + optional `Key`:

  ```go
  reg := outbox.NewRegistry()
  outbox.Register[widget.Created](reg, "widget.created", "widgets", outbox.WithKey("created"))
  ```

- `outbox.Relay` (a `worker.Processor`) drains pending events to any
  `broker.Publisher` with at-least-once delivery; `Sweeper` reclaims leases from
  crashed relays and `Cleaner` prunes terminal rows. The relay paces adaptively
  (`worker.NewPacedLoop`): a full batch drains again immediately, an empty one
  idles for `PollInterval` (optionally backing off to `MaxPollInterval`).
- Metrics are opt-in and registered on the app's shared registry:
  `outbox.NewMetrics` (relay/sweeper/cleaner throughput) + `NewBacklogCollector`
  (pending/in-flight/failed depth and oldest-pending age). SDK and business
  metrics never collide — distinct namespaces on one registry (see `metrics`).
- Each transport maps `Topic`/`Key` to its own concepts (RabbitMQ
  exchange+routing key via `WithTopicMapper`, Kafka topic+partition key, NATS
  subject), so the same event row can ship over any broker. `broker/rabbitmq`
  publishes CloudEvents v1.0 with publisher confirms and consumes them via a
  supervised `worker.Runnable`.

- Tracing flows end-to-end: `Publish` captures the W3C trace context into the
  event headers (`otel.Carrier`), the relay carries it onto the broker message,
  and the consumer restores it with `otel.ExtractFromCarrier` + opens a child
  span — so producing a widget and recording its audit row are one trace.

The `skit-x` showcase wires it end-to-end: creating a widget emits
`widget.Created` through the outbox, the relay publishes it to RabbitMQ, and the
`widgetaudit` consumer records it while continuing the producer's trace (see
`core/widget`, `widgetaudit`, and the e2e test there, which asserts the consumer
span is a child of the producer span).

## Reliable background processing

`worker`, `queue`, and `dbx` compose into an at-least-once batch pipeline:

- `worker.Pool` — bounded-concurrency fan-out of one-shot jobs.
- `worker.Processor[T]` — a `Source` (claim) → `Handler` (process) → `Sink`
  (ack/retry) loop, run on a schedule via `worker.Loop`.
- `queue.PG` — a Postgres queue that hands each ready task to exactly one
  consumer via `FOR UPDATE SKIP LOCKED`, so N replicas drain it safely; it
  satisfies `worker.Source[Task]`/`Sink[Task]` directly.

The showcase wires this end-to-end: `POST /widgets/import` enqueues a batch, and a
supervised import worker bulk-inserts it (idempotently, via `dbx.BulkInsert` +
`ON CONFLICT DO NOTHING`). See `skit-x/core/widgetimport`.

## Splitting one binary across pods

A service usually starts as one process doing everything. When the HTTP side has
to scale, the background loops must not scale with it — N replicas would poll the
same rows N times and any wall-clock cron would fire N times.

The SDK deliberately ships **no role/profile type** for this: it already gives
you two orthogonal gates, and the third question is answered by the command.

| Question | Answered by |
|---|---|
| Is this transport configured at all? | addr-gating — `HTTP_ADDR`, `GRPC_ADDR`, `DEBUG_ADDR` |
| Is this subsystem switched on? | its own flag — `Worker.Disabled`, `Broker.Enabled` |
| Does *this process* run that part, and where? | the command, which lists its options |

The scaffold (`skit new --full`) generates exactly that. A process is assembled
like Lego from options (`internal/app/server/options.go`): `server.New` builds the
dependency container and applies the options the command lists, `app.Run`
supervises what they added. Each long-running command is that list:

```go
// internal/cmd/worker.go
func (c *WorkerCommand) Execute(_ []string) error {
	return run(c.ServerOpts,
		server.WithServer(c.Debug.Addr, server.Debug),
		server.WithWorkers(),
	)
}
```

| Command | Options |
|---|---|
| `serve` | `WithServer(HTTP_ADDR, Install)`, `WithServer(DEBUG_ADDR, Debug)`, `WithWorkers()`, `WithConsumers()` |
| `http` | `WithServer(HTTP_ADDR, Install)`, `WithServer(DEBUG_ADDR, Debug)` |
| `worker` | `WithServer(DEBUG_ADDR, Debug)`, `WithWorkers()` |
| `consumer` | `WithServer(DEBUG_ADDR, Debug)`, `WithConsumers()` |

There is one kind of server: `WithServer(addr, route sets…)` — an `httpserver.Server`
serving the route sets it is given. A "debug server" is not a type but a server with
the technical sets: `Health` (`/healthz`, `/readyz`), `Metrics`, `Pprof`, `Info`
(`/debug/components`), or `Debug` for all of them. `Install` is the business API and
brings its own request middleware chain (trace, access log, HTTP metrics, timeout,
body limit), so a set behaves the same on any server — probes and scrapes never reach
the access log or the request timeout. Moving a set to another port is an edit of the
command, not a config switch:

```go
run(c.ServerOpts,
	server.WithServer(c.HTTP.Addr, server.Install),
	server.WithServer(":9100", server.Metrics),
	server.WithServer(c.Debug.Addr, server.Health, server.Pprof, server.Info),
)
```

No role type, no conditions inside the server package — what a process runs and
where it listens is read off the command. Splitting a deployment is then a
values-only change: the same image with a different command.

Every command keeps a server with `Health`: `/healthz` and `/readyz` are what keep a
worker-only pod probeable and alive until SIGTERM, so one probe configuration fits
every pod. `GET /debug/components` reports what the pod runs — the same values it
logs at startup:

```
{"servers":[":8080",":6060"],"workers":0,"consumers":0}   ← http
{"servers":[":6060"],"workers":3,"consumers":0}           ← worker
```

For a cron that must fire once across the fleet, gate it with `lock.Locker`
(`skit add cron --lock postgres`).

Why it is built this way, what a live split deployment showed (migration races,
requests lost on rollout, the release layout) and what is still open — see
[research-pod-split.md](research-pod-split.md).

## gRPC

`grpcserver` runs alongside (or instead of) the REST server — both are `worker.Runnable`s
supervised by `worker.Group`, toggled independently by config. It ships:

- the same cross-cutting interceptors as REST (panic recovery, trace-id injection, structured
  access logs, Prometheus metrics) plus automatic `*errs.Error` → gRPC `status` mapping (error
  codes are aligned, so the mapping is a direct cast);
- messages use the protobuf **Opaque API** via Protobuf **Editions** (`edition = "2023"`,
  `api_level = API_OPAQUE`) — Google's official path to faster, lower-allocation, lazy-decoding
  generated code (replaces the third-party vtprotobuf; no extra dependency or codec);
- transport tuning (`MaxRecvMsgSize`, `SharedWriteBuffer`, `NumStreamWorkers`, keepalive);
- gRPC health service and optional reflection.

Code is generated with **buf v2** (config lives with the app in `skit-x`). gRPC `v1.81` /
protobuf `v1.36`.

```bash
make proto-tools   # install buf, protoc-gen-go, protoc-gen-go-grpc
make proto         # buf lint + generate in ../skit-x (proto -> gen)
```

## Quick start

```bash
make build      # build the SDK
make test       # unit tests
```

See [`skit-x`](https://github.com/assanoff/skit-x) for a full application wired with
`dim` (`go run ./cmd serve`), plus its integration tests (`make test-integration`).
