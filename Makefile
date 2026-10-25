# go-fabric-server — make targets. `make help` lists them.
GO ?= go
export CGO_ENABLED := 0

MODULE   := github.com/SukramJ/go-fabric-server
PKG_VER  := $(MODULE)/internal/version
BIN_DIR  := bin
DIST_DIR := dist

VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -s -w -X $(PKG_VER).Version=$(VERSION) -X $(PKG_VER).Commit=$(COMMIT) -X $(PKG_VER).BuildDate=$(BUILD_DATE)

# The release matrix (concept v8 B.2 / v7 B.2): linux/amd64, linux/arm64,
# linux/arm/v7. RELEASE_VERSION is internal/version's default — bumping it
# and the changelog section happen in one commit.
RELEASE_TARGETS ?= linux/amd64 linux/arm64 linux/arm/v7
RELEASE_VERSION ?= $(shell awk -F'"' '/^[[:space:]]*Version = /{print $$2; exit}' internal/version/version.go)
RELEASE_PAYLOAD := README.md LICENSE changelog.md THIRD-PARTY-NOTICES.md deploy/systemd/fabric-server.service

DOCKER       ?= docker
DOCKER_IMAGE ?= go-fabric-server
FUZZTIME     ?= 10000x

# Tool versions, pinned as in .github/workflows/ci.yml; bump both together.
GOFUMPT_VERSION       ?= v0.12.0
GOLANGCI_LINT_VERSION ?= v2.14.0
GOVULNCHECK_VERSION   ?= v1.8.0
GOLICENSES_VERSION    ?= v1.6.0
GITLEAKS_VERSION      ?= v8.30.1

.DEFAULT_GOAL := help

help: ## show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-14s %s\n", $$1, $$2}'

setup: ## install the pinned gofumpt, golangci-lint, govulncheck and go-licenses
	$(GO) install mvdan.cc/gofumpt@$(GOFUMPT_VERSION)
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	$(GO) install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	$(GO) install github.com/google/go-licenses@$(GOLICENSES_VERSION)

build: ## go build ./... and the daemon into bin/
	$(GO) build ./...
	$(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/fabric-server ./cmd/fabric-server

vet: ## go vet ./...
	$(GO) vet ./...

test: ## go test -count=1 ./...
	$(GO) test -count=1 -timeout 300s ./...

race: ## go test -race (CGO_ENABLED=1 for this run only)
	CGO_ENABLED=1 $(GO) test -race -count=1 -timeout 600s ./...

fmt: ## gofumpt -w .
	gofumpt -w .

fmt-check: ## fail when a file is not gofumpt-clean
	@diff=$$(gofumpt -l .); if [ -n "$$diff" ]; then echo "gofumpt would rewrite:"; echo "$$diff"; exit 1; fi

lint: fmt-check ## gofumpt check + golangci-lint + go mod tidy -diff + go mod verify
	golangci-lint config verify
	golangci-lint run --timeout=5m ./...
	$(GO) mod tidy -diff
	$(GO) mod verify

tidy-check: ## verify go.mod/go.sum are tidy and the module checksums verify
	$(GO) mod tidy -diff
	$(GO) mod verify

generate: ## regenerate protocol/schema and bridgeapi/clusters_gen.go from go-fabric
	$(GO) run ./internal/gen

cover: ## coverage profile of every package, as the floors count it
	$(GO) test -count=1 -timeout 300s -coverpkg=./... -coverprofile=coverage.out ./...

cover-check: cover ## fail when a package is below its floor (script/coverfloor/floors.txt)
	$(GO) run ./script/coverfloor coverage.out

fuzz-smoke: ## every fuzz target of the JSON decoders, the declaration and the paths, $(FUZZTIME) each
	$(GO) test -run '^$$' -fuzz '^FuzzDecodeRequest$$' -fuzztime $(FUZZTIME) ./protocol/
	$(GO) test -run '^$$' -fuzz '^FuzzParseUint64$$' -fuzztime $(FUZZTIME) ./protocol/
	$(GO) test -run '^$$' -fuzz '^FuzzParsePath$$' -fuzztime $(FUZZTIME) ./internal/wire/
	$(GO) test -run '^$$' -fuzz '^FuzzEncodeFields$$' -fuzztime $(FUZZTIME) ./internal/wire/
	$(GO) test -run '^$$' -fuzz '^FuzzDecodeDeclaration$$' -fuzztime $(FUZZTIME) ./bridgeapi/
	$(GO) test -run '^$$' -fuzz '^FuzzDecodePatch$$' -fuzztime $(FUZZTIME) ./bridgeapi/

vuln: ## govulncheck ./...
	govulncheck ./...

# GOROOT explicit: go-licenses tells the standard library by its GOROOT,
# and a toolchain installed elsewhere than go-licenses was built for (a
# snap, GOTOOLCHAIN) leaves every std package "without module info".
# modernc.org/mathutil's LICENSE is BSD-3-Clause; the classifier does not
# recognise its wording and logs it, without failing the check.
licenses: ## fail on a forbidden, restricted or reciprocal dependency licence (go-licenses)
	GOROOT=$$($(GO) env GOROOT) go-licenses check ./... --disallowed_types=forbidden,restricted,reciprocal

# Git mode: the tracked history, not just the working tree. The server
# stores Wi-Fi passwords and Thread datasets and guards its API with a
# token; the test fixtures carry none.
secrets: ## scan the tracked git history for committed secrets (gitleaks)
	$(GO) run github.com/zricethezav/gitleaks/v8@$(GITLEAKS_VERSION) git --no-banner --redact .

docker: ## build the container image (deploy/Dockerfile)
	$(DOCKER) build -f deploy/Dockerfile --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) --build-arg BUILD_DATE=$(BUILD_DATE) -t $(DOCKER_IMAGE):$(VERSION) .

release: ## stage the cross-compiled release archives and notes into dist/ (no upload)
	@rm -rf $(DIST_DIR) && mkdir -p $(DIST_DIR)
	@echo "release version: $(RELEASE_VERSION)"
	@version="$(RELEASE_VERSION)"; \
	ldflags="-s -w -X $(PKG_VER).Version=$$version -X $(PKG_VER).Commit=$(COMMIT) -X $(PKG_VER).BuildDate=$(BUILD_DATE)"; \
	for tgt in $(RELEASE_TARGETS); do \
	  goos=$$(echo $$tgt | cut -d/ -f1); goarch=$$(echo $$tgt | cut -d/ -f2); goarm=$$(echo $$tgt | cut -d/ -f3 | tr -d v); \
	  name="go-fabric-server-$$version-$$goos-$$goarch$${goarm:+v$$goarm}"; stage="$(DIST_DIR)/$$name"; \
	  mkdir -p "$$stage"; echo "==> $$tgt -> $$stage"; \
	  GOOS=$$goos GOARCH=$$goarch GOARM=$$goarm $(GO) build -trimpath -ldflags="$$ldflags" -o "$$stage/fabric-server" ./cmd/fabric-server || exit 1; \
	  cp $(RELEASE_PAYLOAD) "$$stage/"; \
	  ( cd $(DIST_DIR) && tar -czf "$$name.tar.gz" "$$name" ); rm -rf "$$stage"; \
	done
	@cd $(DIST_DIR) && sha256sum *.tar.gz > SHA256SUMS
	@script/extract-release-notes.sh $(RELEASE_VERSION) > $(DIST_DIR)/RELEASE_NOTES.md
	@ls -lh $(DIST_DIR)

ci: build vet test lint ## everything the pipeline's main jobs run

clean: ## remove build artefacts
	rm -rf $(BIN_DIR) $(DIST_DIR) coverage.out

version: ## print the resolved build metadata
	@echo "VERSION    = $(VERSION)"
	@echo "COMMIT     = $(COMMIT)"
	@echo "BUILD_DATE = $(BUILD_DATE)"

.PHONY: help setup build vet test race fmt fmt-check lint tidy-check generate cover cover-check fuzz-smoke vuln licenses secrets docker release ci clean version
