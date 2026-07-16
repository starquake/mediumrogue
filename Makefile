SHELL = /bin/bash -o pipefail

ifneq (,$(wildcard ./.env))
    include .env
    export
endif

# Go lives at /usr/local/go/bin on machines where it is not on PATH.
GO := $(shell command -v go 2>/dev/null || echo /usr/local/go/bin/go)

BUILD_DIR := build
BIN_DIR   := $(BUILD_DIR)/bin
ABS_BIN   := $(abspath $(BIN_DIR))

# golangci-lint version + binary path. Must match `with: version:` in the lint
# job of .github/workflows/ci.yml — bump both together.
GOLANGCI_VERSION := v2.12.2
GOLANGCI_BIN     := $(BIN_DIR)/golangci-lint

# tygo is built from tools/ (where dependabot tracks its version).
TYGO_BIN := $(BIN_DIR)/tygo

# Developer gate before committing. Mirrors the CI jobs.
.PHONY: check
check: lint docs-check protocol-check icons-check client-check test test-integration build

# Dead-reference check over all tracked markdown: every repo path a doc
# mentions (backtick-quoted or relatively linked) must exist. Runs in CI as
# the `docs` job — the check that actually validates a docs-only PR (#125).
.PHONY: docs-check
docs-check:
	./scripts/docs-check.sh

## ---- Go server ----

.PHONY: build
build: client
	$(GO) build -o $(BIN_DIR)/rogue ./cmd/rogue

# Build the server without rebuilding the client bundle (uses whatever is in
# internal/web/dist — possibly just the .gitkeep placeholder).
.PHONY: build-server
build-server:
	$(GO) build -o $(BIN_DIR)/rogue ./cmd/rogue

.PHONY: server
server:
	$(GO) run ./cmd/rogue

.PHONY: smoke
smoke:
	$(GO) run ./cmd/rogue -check

.PHONY: test
test:
	$(GO) test ./cmd/... ./internal/...

.PHONY: test-integration
test-integration:
	$(GO) test ./test/integration/...

# Run the Go server with auto-restart on source changes. Pair with
# `make client-dev` in a second terminal; Vite proxies /api here.
.PHONY: dev
dev:
	@command -v watchexec >/dev/null 2>&1 || { echo "watchexec not found — install from https://github.com/watchexec/watchexec"; exit 1; }
	watchexec \
	    --restart \
	    --stop-signal SIGTERM \
	    --shell=none \
	    --watch cmd \
	    --watch internal \
	    --watch go.mod \
	    -- $(GO) run ./cmd/rogue

## ---- Protocol (Go -> TypeScript) ----

$(TYGO_BIN): tools/go.mod tools/go.sum
	cd tools && $(GO) build -o $(abspath $(TYGO_BIN)) github.com/gzuidhof/tygo

.PHONY: protocol
protocol: $(TYGO_BIN)
	$(TYGO_BIN) generate

# Fail when client/src/protocol.gen.ts is out of sync with
# internal/protocol — the contract-drift gate.
.PHONY: protocol-check
protocol-check: protocol
	@git diff --exit-code -- client/src/protocol.gen.ts \
	    || { echo "protocol.gen.ts is stale: commit the regenerated file"; exit 1; }

## ---- Glyph icons (vendored game-icons.net SVGs -> glyphIcons.ts) ----

# Regenerate client/src/render/glyphIcons.ts from the source SVGs in
# client/tools/glyph-icons/svg. Run after swapping a source icon.
.PHONY: icons
icons:
	node client/tools/glyph-icons/gen-glyph-icons.mjs

# Fail when glyphIcons.ts is out of sync with its source SVGs — the same
# generate-and-check-in drift gate as protocol-check.
.PHONY: icons-check
icons-check: icons
	@git diff --exit-code -- client/src/render/glyphIcons.ts \
	    || { echo "glyphIcons.ts is stale: run 'make icons' and commit"; exit 1; }

## ---- Client (Vite + TypeScript + PixiJS) ----

client/node_modules: client/package.json client/package-lock.json
	cd client && npm ci
	@touch client/node_modules

.PHONY: client
client: client/node_modules protocol
	cd client && npm run build
	@touch internal/web/dist/.gitkeep

.PHONY: client-check
client-check: client/node_modules protocol
	cd client && npm run check && npm run test

.PHONY: client-dev
client-dev: client/node_modules
	cd client && npm run dev

## ---- End-to-end ----

# Playwright drives the real embedded-client binary with a fast clock.
# First run needs browsers: cd client && npx playwright install chromium
.PHONY: e2e
e2e: build
	cd client && npm run e2e

## ---- Lint ----

$(GOLANGCI_BIN):
	@mkdir -p $(BIN_DIR)
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh \
	    | sh -s -- -b $(ABS_BIN) $(GOLANGCI_VERSION)

.PHONY: lint
lint: $(GOLANGCI_BIN)
	$(GOLANGCI_BIN) run

.PHONY: lint-fix
lint-fix: $(GOLANGCI_BIN)
	$(GOLANGCI_BIN) run --fix

# Advisory (from topbanana): flags server-implementation Go that names the
# client layer ("frontend"/"client-side"). The server owns simulation, the
# client owns rendering; they meet only at internal/protocol, which is exempt
# as the shared wire contract (like topbanana exempts its generated db package).
# Prints hits, never fails the build; not wired into `check`.
.PHONY: lint-cross-refs
lint-cross-refs:
	@hits=$$(grep -rniE '\b(frontend|client-side)\b' \
	    --include='*.go' --exclude='*_test.go' --exclude-dir=protocol \
	    internal/ cmd/ 2>/dev/null || true); \
	if [ -n "$$hits" ]; then \
	    echo "lint-cross-refs: server implementation names the client layer (advisory):"; \
	    echo "$$hits" | sed 's/^/  /'; \
	else \
	    echo "lint-cross-refs: no client-layer references in server implementation."; \
	fi

# Advisory (from topbanana): flags any *_test.go without a same-named *.go
# (stdlib source/test pairing). Exempts export_test.go, testmain_test.go, and
# any *_test.go with no Test/Benchmark/Example func (test-only helpers).
# Prints offenders, never fails the build; not wired into `check`.
.PHONY: lint-test-pairing
lint-test-pairing:
	@hits=$$(for t in $$(find internal cmd -name '*_test.go' 2>/dev/null); do \
	    d=$$(dirname "$$t"); b=$$(basename "$$t" _test.go); \
	    case "$$b" in export|testmain) continue;; esac; \
	    [ -e "$$d/$$b.go" ] && continue; \
	    grep -qE '^func (Test|Benchmark|Example)' "$$t" || continue; \
	    echo "$$t"; \
	  done); \
	if [ -n "$$hits" ]; then \
	    echo "lint-test-pairing: these test files have no same-named source file:"; \
	    echo "$$hits" | sed 's/^/  /'; \
	else \
	    echo "lint-test-pairing: every test file pairs a source file."; \
	fi

.PHONY: clean
clean:
	rm -rf $(BUILD_DIR) client/node_modules client/test-results
	find internal/web/dist -mindepth 1 -not -name .gitkeep -delete
