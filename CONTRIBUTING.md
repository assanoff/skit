# Contributing to skit

Thanks for your interest in improving skit. It is a single Go module
(`github.com/assanoff/skit`); the runnable showcase lives in a separate repo,
[`skit-x`](https://github.com/assanoff/skit-x), which imports this SDK.

## Prerequisites

- **Go 1.26+** (see `go.mod`).
- `make tools` installs the dev tools (golangci-lint, gofumpt, gonew, gorelease,
  and the protobuf codegen tools).
- Docker — only for integration tests (which run in the showcase repo).

## Development workflow

```bash
make build   # compile all packages
make vet     # go vet ./...
make test    # unit tests (short, race) with per-package coverage
make lint    # golangci-lint
make fmt     # gofumpt + goimports
```

Run `make build vet test lint` before opening a pull request. CI runs the same
checks on every push to `master` and on pull requests, plus a `gorelease` API
diff (see [RELEASING.md](RELEASING.md)).

## Conventions

- **One `doc.go` per package** — every package carries a package-level doc
  comment (overview + a short usage example). New packages must add one.
- **Exported symbols are documented** — enforced by the linter (`revive`,
  `exported`).
- **Tests use [`matryer/is`](https://github.com/matryer/is)** (not testify).
  Integration tests use the `dbtest` helper / testcontainers.
- **Pass dependencies explicitly** — thread `*sqlx.DB`, transactions, loggers,
  and other dependencies as function arguments; never smuggle them through
  `context.Context`.
- **Keep the SDK self-contained** — no references to private or upstream source
  projects in code, comments, or docs.
- Match the surrounding code: naming, functional options, and error handling via
  the `errs` package.

## Commits & pull requests

- Keep changes focused, and explain the *what* and *why*.
- Add or update tests for any behavior change.
- Keep `go.mod`/`go.sum` tidy (`make tidy`).
- The public API is diffed against the latest tag by `gorelease`. Breaking
  changes are allowed on `v0.x` but should be called out in the PR description.

## Reporting issues

- **Bugs / features:** open a GitHub issue with a minimal reproduction.
- **Security vulnerabilities:** do **not** open a public issue — follow
  [SECURITY.md](SECURITY.md).

## License

By contributing you agree that your contributions are licensed under the
repository's [MIT License](LICENSE).
