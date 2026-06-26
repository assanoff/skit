.DEFAULT_GOAL := help

GO          ?= go
MODULES     := .
LINT        ?= golangci-lint
# The runnable showcase app (full CRUD, integration tests, proto codegen) lives
# in a sibling repo that imports this SDK; some targets below drive it there.
SHOWCASE    ?= ../skit-x

# Dev tool versions (override to pin, e.g. GORELEASE=...@v0.0.0-20240...).
GOLANGCI    ?= github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
GONEW       ?= golang.org/x/tools/cmd/gonew@latest
GORELEASE   ?= golang.org/x/exp/cmd/gorelease@latest
GOFUMPT     ?= mvdan.cc/gofumpt@latest
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

.PHONY: tidy
tidy: ## Run go mod tidy in all modules
	@for m in $(MODULES); do echo ">> tidy $$m"; (cd $$m && $(GO) mod tidy); done

.PHONY: build
build: ## Build all modules
	@for m in $(MODULES); do echo ">> build $$m"; (cd $$m && $(GO) build ./...); done

# Build the CLI into bin/ (gitignored), NEVER bare `go build ./cmd/skit`,
# which would drop a `skit` binary at the repo root.
.PHONY: build-cli
build-cli: ## Build the skit CLI into bin/ (gitignored)
	$(GO) build -o bin/skit ./cmd/skit
	@echo "built bin/skit"

.PHONY: vet
vet: ## go vet all modules
	@for m in $(MODULES); do echo ">> vet $$m"; (cd $$m && $(GO) vet ./...); done

.PHONY: test
test: ## Run unit tests (short, race) with per-package coverage
	@for m in $(MODULES); do echo ">> test $$m"; (cd $$m && $(GO) test -race -short -cover ./...); done

.PHONY: test-json
test-json: ## Unit tests with a pretty pass/fail + coverage summary (tparse)
	@bash -o pipefail -c '$(GO) test -short -race -cover ./... -json | $(GO) run $(TPARSE) -all'

.PHONY: cover
cover: ## Write a coverage profile, print the total, and render the HTML
	@mkdir -p $(COVERDIR)
	@for m in $(MODULES); do (cd $$m && $(GO) test -short -covermode=atomic -coverprofile=$(COVERPROFILE) ./...); done
	@$(GO) tool cover -func=$(COVERPROFILE) | tail -n1
	@$(GO) tool cover -html=$(COVERPROFILE) -o $(COVERHTML) && echo ">> wrote $(COVERHTML)"

.PHONY: cover-check
cover-check: ## Fail if total unit-test coverage is below THRESHOLD% (CI gate; override THRESHOLD=NN)
	@mkdir -p $(COVERDIR)
	@for m in $(MODULES); do (cd $$m && $(GO) test -short -covermode=atomic -coverprofile=$(COVERPROFILE) ./...); done
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
lint: ## Run golangci-lint in all modules
	@for m in $(MODULES); do echo ">> lint $$m"; (cd $$m && $(LINT) run); done

.PHONY: tools
tools: proto-tools ## Install all dev tools (lint, fmt, scaffold, release, proto)
	$(GO) install $(GOLANGCI)
	$(GO) install $(GONEW)
	$(GO) install $(GORELEASE)
	$(GO) install $(GOFUMPT)
	$(GO) install $(TPARSE)
	@echo "installed: golangci-lint, gonew, gorelease, gofumpt, tparse (+ proto tools)"

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

.PHONY: fmt
fmt: ## Format code
	@for m in $(MODULES); do (cd $$m && $(GO) tool gofumpt -w . 2>/dev/null || gofmt -w .); done

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
