.DEFAULT_GOAL := help

GO          ?= go
# The runnable showcase app (full CRUD, integration tests, proto codegen) lives
# in a sibling repo that imports this SDK; some targets below drive it there.
SHOWCASE    ?= ../skit-x

# Gate and CI tools are pinned and run via `go run pkg@version`, nothing to
# install. GOLANGCI matches the golangci-lint version in .github/workflows/ci.yml.
GOLANGCI    ?= github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
GOVULNCHECK ?= golang.org/x/vuln/cmd/govulncheck@v1.9.0
GORELEASE   ?= golang.org/x/exp/cmd/gorelease@v0.0.0-20261009195045-ca0d7ba23607
# Plain `=`, not `?=`: GNU make 3.81 (macOS) predefines LINT = lint, which `?=`
# does not override. A local binary still works: make lint LINT=golangci-lint.
LINT         = $(GO) run $(GOLANGCI)

# Convenience tools outside any gate; latest is fine.
GONEW       ?= golang.org/x/tools/cmd/gonew@latest
TPARSE      ?= github.com/mfridman/tparse@latest

# COVERDIR is the gitignored directory all coverage artifacts live in, so they
# never clutter the repo root. THRESHOLD is the `cover-check` CI gate.
COVERDIR     ?= coverdata
COVERPROFILE ?= $(COVERDIR)/coverage.out
COVERHTML    := $(COVERDIR)/coverage.html
THRESHOLD    ?= 35

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

.PHONY: check
check: tidy-check fix-check lint lint-rules vuln test ## Everything CI checks

.PHONY: tidy
tidy: ## Run go mod tidy
	$(GO) mod tidy

.PHONY: tidy-check
tidy-check: ## go.mod and go.sum are tidy, the module cache is untampered
	$(GO) mod tidy -diff
	$(GO) mod verify

.PHONY: build
build: ## Build the SDK
	$(GO) build ./...

# Build the CLI into bin/ (gitignored), NEVER bare `go build ./cmd/skit`,
# which would drop a `skit` binary at the repo root.
.PHONY: build-cli
build-cli: ## Build the skit CLI into bin/ (gitignored)
	$(GO) build -o bin/skit ./cmd/skit
	@echo "built bin/skit"

.PHONY: vet
vet: ## go vet
	$(GO) vet ./...

.PHONY: test
test: ## Unit tests (short, race, random order) with per-package coverage
	$(GO) test -race -short -shuffle=on -cover ./...

.PHONY: test-json
test-json: ## Unit tests with a pretty pass/fail + coverage summary (tparse)
	@bash -o pipefail -c '$(GO) test -short -race -shuffle=on -cover ./... -json | $(GO) run $(TPARSE) -all'

.PHONY: cover
cover: ## Write a coverage profile, print the total, and render the HTML
	@mkdir -p $(COVERDIR)
	@$(GO) test -short -covermode=atomic -coverprofile=$(COVERPROFILE) ./...
	@$(GO) tool cover -func=$(COVERPROFILE) | tail -n1
	@$(GO) tool cover -html=$(COVERPROFILE) -o $(COVERHTML) && echo ">> wrote $(COVERHTML)"

.PHONY: cover-check
cover-check: ## Fail if total unit-test coverage is below THRESHOLD% (CI gate; override THRESHOLD=NN)
	@mkdir -p $(COVERDIR)
	@$(GO) test -short -covermode=atomic -coverprofile=$(COVERPROFILE) ./...
	@total=$$($(GO) tool cover -func=$(COVERPROFILE) | awk '/^total:/ {print $$3}' | tr -d '%'); \
	if awk "BEGIN { exit !($$total + 0 >= $(THRESHOLD)) }"; then \
		echo ">> coverage $$total% >= $(THRESHOLD)% — OK"; \
	else \
		echo ">> coverage $$total% < $(THRESHOLD)% — FAIL"; exit 1; \
	fi

.PHONY: test-integration
test-integration: ## Run integration tests in the showcase app (requires docker + $(SHOWCASE))
	cd $(SHOWCASE) && $(GO) test -race -count=1 ./internal/tests/...

.PHONY: lint
lint: ## Run golangci-lint
	$(LINT) run

.PHONY: fmt
fmt: ## Format with the .golangci.yml formatters (gofumpt, goimports)
	$(LINT) fmt

# `run --fix` exits non-zero while findings without an autofix remain; the
# leading `-` lets formatting still run. Review the diff: an autofix can be
# wrong (errorlint on a recover() value, fatcontext shadowing a variable).
.PHONY: fix
fix: ## Apply go fix, linter autofixes and formatting
	$(GO) fix ./...
	-$(LINT) run --fix
	$(LINT) fmt

.PHONY: fix-check
fix-check: ## go fix -diff: modernizations and //go:fix inline are applied
	$(GO) fix -diff ./...

.PHONY: lint-rules
lint-rules: ## Project rules: no else, one-line functions fit in 80 columns
	@scripts/lint-rules.sh

.PHONY: vuln
vuln: ## Known vulnerabilities in reachable code
	$(GO) run $(GOVULNCHECK) ./...

.PHONY: tools
tools: proto-tools ## Install convenience dev tools (scaffold, test output, proto)
	$(GO) install $(GONEW)
	$(GO) install $(TPARSE)
	@echo "installed: gonew, tparse (+ proto tools)"

.PHONY: proto-tools
proto-tools: ## Install protobuf codegen tools (buf, protoc-gen-go, protoc-gen-go-grpc, grpc-gateway)
	$(GO) install github.com/bufbuild/buf/cmd/buf@latest
	$(GO) install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	$(GO) install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	$(GO) install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@latest

.PHONY: proto
proto: ## Generate gRPC + gateway code in the showcase app (run proto-tools first)
	# Workspace mode (no path arg) so buf.yaml deps (googleapis) resolve for the
	# google.api.http annotations the gateway plugin needs.
	cd $(SHOWCASE) && buf dep update && buf lint && buf generate

# gorelease analyzes the committed tree, so a release must start from a clean
# working tree. This also keeps the tag pointing at exactly what was verified.
.PHONY: check-clean
check-clean:
	@test -z "$$(git status --porcelain)" || \
		{ echo "working tree is dirty — commit (or stash) changes before releasing"; \
		  git status --short; exit 1; }

.PHONY: gorelease
gorelease: check-clean ## Show the API diff vs the latest tag + gorelease's suggested next version
	@$(GO) run $(GORELEASE)

.PHONY: release-suggest
release-suggest: check-clean ## Print just the version gorelease suggests for the next release
	@$(GO) run $(GORELEASE) 2>/dev/null | sed -n 's/^Suggested version: //p'

# check-version enforces that a hand-picked VERSION is exactly one increment
# above the latest tag — a patch, minor, or major step. This rejects a version
# that is lower than, equal to, or skips ahead of the current one. gorelease
# (run in `release`) separately enforces the semantic FLOOR (e.g. a breaking
# change on v1+ must be a major); together they pin the version to one sane step.
.PHONY: check-version
check-version:
	@test -n "$(VERSION)" || { echo "usage: make release VERSION=vX.Y.Z (or: make release-auto)"; exit 1; }
	@echo "$(VERSION)" | grep -qE '^v[0-9]+\.[0-9]+\.[0-9]+$$' || \
		{ echo "VERSION must be vX.Y.Z (no prerelease/build suffix): got $(VERSION)"; exit 1; }
	@cur=$$(git tag --list 'v*' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$$' | sort -V | tail -n1); \
	if [ -z "$$cur" ]; then \
		echo ">> first release ($(VERSION)); no prior tag to compare against"; \
	else \
		cv=$${cur#v}; cM=$${cv%%.*}; cr=$${cv#*.}; cm=$${cr%%.*}; cp=$${cr##*.}; \
		np="v$$cM.$$cm.$$((cp + 1))"; nm="v$$cM.$$((cm + 1)).0"; nj="v$$((cM + 1)).0.0"; \
		case "$(VERSION)" in \
			"$$np"|"$$nm"|"$$nj") echo ">> $(VERSION) is exactly one step above $$cur" ;; \
			*) echo "ERROR: $(VERSION) must be exactly one step above the latest tag $$cur"; \
			   echo "       allowed: $$np (patch) | $$nm (minor) | $$nj (major)"; exit 1 ;; \
		esac; \
	fi

.PHONY: release
release: check-clean check-version ## Tag & push a release (one step up, gorelease-validated): make release VERSION=v0.1.0
	@echo ">> verifying build & tests"; $(GO) build ./... && $(GO) test -short ./...
	@echo ">> validating $(VERSION) against the public API (gorelease)"; \
		$(GO) run $(GORELEASE) -version=$(VERSION)
	@echo ">> tagging $(VERSION)"; git tag -a $(VERSION) -m "Release $(VERSION)"
	@echo ">> pushing $(VERSION)"; git push origin $(VERSION)
	@echo "release workflow will build the CLI and publish the GitHub Release"

.PHONY: release-auto
release-auto: check-clean ## Tag & push the exact version gorelease suggests from the API diff
	@v=$$($(GO) run $(GORELEASE) 2>/dev/null | sed -n 's/^Suggested version: //p'); \
	if [ -z "$$v" ]; then \
		echo "gorelease did not suggest a version — likely incompatible changes that need"; \
		echo "a new major version (v2+ module path). Run 'make gorelease' for details."; \
		exit 1; \
	fi; \
	echo ">> gorelease suggests $$v"; \
	$(MAKE) release VERSION=$$v
