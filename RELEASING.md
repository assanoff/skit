# Releasing skit

skit is a single-module repo: the **root module**
(`github.com/assanoff/skit`) is the only thing published and versioned.
The runnable showcase application lives in a separate repo,
[`skit-x`](https://github.com/assanoff/skit-x), which imports this
SDK from GitHub — it is not part of this module and is never tagged here.

## Tooling

Install the dev tools (golangci-lint, gonew, gorelease, gofumpt, + proto):

```bash
make tools
```

## Versioning with gorelease

The SDK follows [semantic import versioning](https://go.dev/ref/mod#versions).
[`gorelease`](https://pkg.go.dev/golang.org/x/exp/cmd/gorelease) picks the
version for you by diffing the **committed** public API against the base
version (`apidiff` under the hood) and applying semver rules:

- **No API changes** → patch bump (e.g. `v1.2.3` → `v1.2.4`).
- **Backward-compatible additions** → minor bump (`v1.2.3` → `v1.3.0`).
- **Incompatible (breaking) changes**:
  - on **v0.x** they are allowed — gorelease reports them but still suggests a
    minor bump and exits 0 (v0 carries no compatibility guarantee);
  - on **v1+** they are forbidden in the same major — gorelease exits non-zero;
    the change must go to a new major version with a `/v2` module path.

gorelease infers the base from the latest tag automatically (or `none` before
the first release, where it suggests `v0.1.0`). It **requires a clean working
tree**, so commit first.

```bash
make gorelease         # full API diff + "Suggested version: vX.Y.Z"
make release-suggest   # just the suggested version string
```

CI runs `gorelease` on every push: on v1+ an unaccounted breaking change fails
the build before it ships.

## Cutting a release

1. Ensure `master` is green (build, vet, test, lint, integration).
2. Commit everything — gorelease and tagging operate on the committed tree.
3. Release. The `release` workflow then runs the tests, builds the `skit`
   CLI for linux/darwin (amd64/arm64), and publishes a GitHub Release with
   checksums and auto-generated notes:

   ```bash
   # let gorelease choose the version from the API diff:
   make release-auto

   # or pin it explicitly (validated by gorelease before tagging):
   make release VERSION=v0.7.0
   ```

   Either path verifies build+tests, validates the version against the public
   API, then `git tag`s and pushes — which triggers the release workflow.

A hand-picked `VERSION` must be **exactly one step above the latest tag** — the
next patch, minor, or major (`v1.2.3` → `v1.2.4` | `v1.3.0` | `v2.0.0`). A
version that is lower, equal, or skips ahead is rejected before tagging. This
is the monotonic guard; `gorelease` separately enforces the semantic *floor*
(e.g. a breaking change on v1+ must be a major bump). `release-auto` always
satisfies both, since it uses gorelease's suggestion.

## Consuming a release

Once published, downstream services depend on the SDK normally:

```bash
go get github.com/assanoff/skit@v0.6.0
```

Scaffold a new service with the CLI (no `replace` needed — it pins the SDK
version in the generated `go.mod`):

```bash
go install github.com/assanoff/skit/cmd/skit@v0.6.0
skit new github.com/you/svc --sdk-version v0.6.0
```

## The showcase application

The showcase lives in its own repo,
[`skit-x`](https://github.com/assanoff/skit-x), and is not part of
this module — nothing here needs to be excluded from releases. For local
co-development it pins the SDK with a `replace`:

```
replace github.com/assanoff/skit => ../skit
```

Drop that `replace` (or pin a tag) to build against a published version.
