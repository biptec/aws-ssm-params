SHELL := /bin/bash
.SHELLFLAGS := -euo pipefail -c

.DEFAULT_GOAL := help

GO ?= go
PKG_DIRS ?= $(shell $(GO) list -f '{{.Dir}}' ./...)
PKGS ?= $(patsubst $(CURDIR)/%,./%,$(filter-out $(CURDIR)/vendor/%,$(PKG_DIRS)))
VERSION ?= $(shell git describe --tags --exact-match 2>/dev/null || git describe --tags --dirty --always 2>/dev/null || echo dev)
VERSION_VAR := main.version
GO_LDFLAGS ?= -X $(VERSION_VAR)=$(VERSION)

# Keep checks offline-safe by default: Go will not auto-download another toolchain.
# Override when you explicitly want Go toolchain auto-downloads:
#   GOTOOLCHAIN=auto make check
GOTOOLCHAIN ?= local
export GOTOOLCHAIN

BIN_DIR := $(CURDIR)/bin
export PATH := $(BIN_DIR):$(PATH)

GOLANGCI_LINT_VERSION ?= v2.12.2
GOLANGCI_MODULES_DOWNLOAD_MODE ?= $(if $(wildcard vendor/modules.txt),vendor,mod)
FORCE_LOCAL_TOOLS ?= false

GOLANGCI_LINT ?= golangci-lint
GOIMPORTS ?= goimports
GOFUMPT ?= gofumpt
GOVULNCHECK ?= govulncheck
GOVULNDB ?=

COVERAGE_FILE ?= coverage.out
COVERAGE_HTML ?= coverage.html
COVERAGE_MIN ?= 70

GO_FILE_FIND := find . \
	-type f \
	-name '*.go' \
	-not -path './vendor/*' \
	-not -path './.git/*' \
	-not -path './bin/*'

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  make tools            Check required tools without installing anything"
	@echo "  make doctor           Same as tools"
	@echo "  make install-tools    Install missing tools into ./bin"
	@echo "  make ci-check         Install missing tools, then run the normal quality gate"
	@echo "  make fix              Format code and tidy modules"
	@echo "  make fmt              Format Go files with goimports + gofumpt"
	@echo "  make fmt-check        Check formatting and imports"
	@echo "  make mod-tidy         Run go mod tidy"
	@echo "  make mod-check        Check go.mod/go.sum consistency without keeping changes"
	@echo "  make vet              Run go vet"
	@echo "  make build            Build all packages"
	@echo "  make test             Run tests with cache disabled and shuffle enabled"
	@echo "  make test-race        Run tests with race detector"
	@echo "  make coverage         Run tests and enforce coverage threshold"
	@echo "  make coverage-html    Generate HTML coverage report"
	@echo "  make lint             Run golangci-lint"
	@echo "  make vuln             Run govulncheck"
	@echo "  make check            Run normal quality gate without installing tools"
	@echo "  make deep-check       Run strict quality gate with race + coverage"
	@echo "  make clean            Remove generated files"
	@echo ""
	@echo "Config:"
	@echo "  GOTOOLCHAIN=$(GOTOOLCHAIN)"
	@echo "  VERSION=$(VERSION)"
	@echo "  COVERAGE_MIN=$(COVERAGE_MIN)"
	@echo "  FORCE_LOCAL_TOOLS=$(FORCE_LOCAL_TOOLS)"
	@echo "  GOLANGCI_MODULES_DOWNLOAD_MODE=$(GOLANGCI_MODULES_DOWNLOAD_MODE)"

.PHONY: require-go
require-go:
	@command -v $(GO) >/dev/null 2>&1 || { \
		echo "go not found. Install Go first."; \
		exit 1; \
	}

.PHONY: require-curl
require-curl:
	@command -v curl >/dev/null 2>&1 || { \
		echo "curl not found. Install curl or install golangci-lint manually."; \
		exit 1; \
	}

.PHONY: require-goimports
require-goimports:
	@command -v $(GOIMPORTS) >/dev/null 2>&1 || { \
		echo "goimports not found."; \
		echo "Install it with: make install-tools"; \
		exit 1; \
	}

.PHONY: require-gofumpt
require-gofumpt:
	@command -v $(GOFUMPT) >/dev/null 2>&1 || { \
		echo "gofumpt not found."; \
		echo "Install it with: make install-tools"; \
		exit 1; \
	}

.PHONY: require-govulncheck
require-govulncheck:
	@command -v $(GOVULNCHECK) >/dev/null 2>&1 || { \
		echo "govulncheck not found."; \
		echo "Install it with: make install-tools"; \
		exit 1; \
	}

.PHONY: require-golangci-lint
require-golangci-lint:
	@command -v $(GOLANGCI_LINT) >/dev/null 2>&1 || { \
		echo "golangci-lint not found."; \
		echo "Install it with: make install-tools"; \
		exit 1; \
	}

.PHONY: tools doctor
tools: doctor

doctor: require-go
	@echo "Go: $$($(GO) env GOVERSION)"
	@missing=0; \
	for tool in $(GOIMPORTS) $(GOFUMPT) $(GOVULNCHECK) $(GOLANGCI_LINT); do \
		if command -v "$$tool" >/dev/null 2>&1; then \
			echo "OK: $$tool -> $$(command -v "$$tool")"; \
		else \
			echo "MISSING: $$tool"; \
			missing=1; \
		fi; \
	done; \
	if [[ "$$missing" -ne 0 ]]; then \
		echo ""; \
		echo "Install missing tools with: make install-tools"; \
		exit 1; \
	fi

.PHONY: install-tools
install-tools: install-goimports install-gofumpt install-govulncheck install-golangci-lint

$(BIN_DIR):
	@mkdir -p $(BIN_DIR)

.PHONY: install-goimports
install-goimports: require-go | $(BIN_DIR)
	@if [[ "$(FORCE_LOCAL_TOOLS)" != "true" ]] && command -v $(GOIMPORTS) >/dev/null 2>&1; then \
		echo "goimports already available: $$(command -v $(GOIMPORTS))"; \
	else \
		echo "Installing goimports into $(BIN_DIR)..."; \
		GOBIN=$(BIN_DIR) $(GO) install golang.org/x/tools/cmd/goimports@latest; \
	fi

.PHONY: install-gofumpt
install-gofumpt: require-go | $(BIN_DIR)
	@if [[ "$(FORCE_LOCAL_TOOLS)" != "true" ]] && command -v $(GOFUMPT) >/dev/null 2>&1; then \
		echo "gofumpt already available: $$(command -v $(GOFUMPT))"; \
	else \
		echo "Installing gofumpt into $(BIN_DIR)..."; \
		GOBIN=$(BIN_DIR) $(GO) install mvdan.cc/gofumpt@latest; \
	fi

.PHONY: install-govulncheck
install-govulncheck: require-go | $(BIN_DIR)
	@if [[ "$(FORCE_LOCAL_TOOLS)" != "true" ]] && command -v $(GOVULNCHECK) >/dev/null 2>&1; then \
		echo "govulncheck already available: $$(command -v $(GOVULNCHECK))"; \
	else \
		echo "Installing govulncheck into $(BIN_DIR)..."; \
		GOBIN=$(BIN_DIR) $(GO) install golang.org/x/vuln/cmd/govulncheck@latest; \
	fi

.PHONY: install-golangci-lint
install-golangci-lint: require-curl | $(BIN_DIR)
	@if [[ "$(FORCE_LOCAL_TOOLS)" != "true" ]] && command -v $(GOLANGCI_LINT) >/dev/null 2>&1; then \
		echo "golangci-lint already available: $$(command -v $(GOLANGCI_LINT))"; \
	else \
		echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION) into $(BIN_DIR)..."; \
		curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b $(BIN_DIR) $(GOLANGCI_LINT_VERSION); \
	fi

.PHONY: fix
fix: fmt mod-tidy

.PHONY: fmt
fmt: require-goimports require-gofumpt
	@echo "Formatting Go files with goimports..."
	@$(GO_FILE_FIND) -print0 | while IFS= read -r -d '' file; do \
		$(GOIMPORTS) -w "$$file"; \
	done
	@echo "Formatting Go files with gofumpt..."
	@$(GO_FILE_FIND) -print0 | while IFS= read -r -d '' file; do \
		$(GOFUMPT) -w "$$file"; \
	done

.PHONY: fmt-check
fmt-check: require-goimports require-gofumpt
	@echo "Checking goimports..."
	@bad_files="$$( \
		$(GO_FILE_FIND) -print0 | while IFS= read -r -d '' file; do \
			$(GOIMPORTS) -l "$$file"; \
		done \
	)"; \
	if [[ -n "$$bad_files" ]]; then \
		echo "Files with incorrect imports or formatting:"; \
		echo "$$bad_files"; \
		echo "Run: make fmt"; \
		exit 1; \
	fi
	@echo "Checking gofumpt..."
	@bad_files="$$( \
		$(GO_FILE_FIND) -print0 | while IFS= read -r -d '' file; do \
			$(GOFUMPT) -l "$$file"; \
		done \
	)"; \
	if [[ -n "$$bad_files" ]]; then \
		echo "Files not formatted with gofumpt:"; \
		echo "$$bad_files"; \
		echo "Run: make fmt"; \
		exit 1; \
	fi

.PHONY: mod-tidy
mod-tidy: require-go
	@echo "Running go mod tidy..."
	@$(GO) mod tidy

.PHONY: mod-check
mod-check: require-go
	@echo "Checking go.mod/go.sum consistency..."
	@if [[ ! -f go.mod ]]; then \
		echo "go.mod not found"; \
		exit 1; \
	fi
	@tmp="$$(mktemp -d)"; \
	cp go.mod "$$tmp/go.mod"; \
	if [[ -f go.sum ]]; then cp go.sum "$$tmp/go.sum"; fi; \
	restore() { \
		cp "$$tmp/go.mod" go.mod; \
		if [[ -f "$$tmp/go.sum" ]]; then cp "$$tmp/go.sum" go.sum; else rm -f go.sum; fi; \
		rm -rf "$$tmp"; \
	}; \
	trap restore EXIT; \
	$(GO) mod tidy; \
	status=0; \
	cmp -s go.mod "$$tmp/go.mod" || status=1; \
	if [[ -f go.sum && -f "$$tmp/go.sum" ]]; then \
		cmp -s go.sum "$$tmp/go.sum" || status=1; \
	elif [[ -f go.sum || -f "$$tmp/go.sum" ]]; then \
		status=1; \
	fi; \
	trap - EXIT; \
	restore; \
	if [[ "$$status" -ne 0 ]]; then \
		echo "go.mod/go.sum are not tidy."; \
		echo "Run: make mod-tidy"; \
		exit 1; \
	fi

.PHONY: vet
vet: require-go
	@echo "Running go vet..."
	@$(GO) vet $(PKGS)

.PHONY: build
build: require-go
	@echo "Building all packages..."
	@$(GO) build -ldflags "$(GO_LDFLAGS)" $(PKGS)

.PHONY: test
test: require-go
	@echo "Running tests..."
	@$(GO) test -count=1 -shuffle=on $(PKGS)

.PHONY: test-race
test-race: require-go
	@echo "Running tests with race detector..."
	@$(GO) test -count=1 -race $(PKGS)

.PHONY: coverage
coverage: require-go
	@echo "Running tests with coverage..."
	@$(GO) test -count=1 -covermode=atomic -coverprofile=$(COVERAGE_FILE) $(PKGS)
	@total="$$( \
		$(GO) tool cover -func=$(COVERAGE_FILE) | \
		awk '/^total:/ { gsub(/%/, "", $$3); print $$3 }' \
	)"; \
	echo "Total coverage: $$total%"; \
	awk -v total="$$total" -v min="$(COVERAGE_MIN)" \
		'BEGIN { exit !(total + 0 >= min + 0) }' || { \
			echo "Coverage $$total% is below required $(COVERAGE_MIN)%"; \
			exit 1; \
		}

.PHONY: coverage-html
coverage-html: coverage
	@echo "Generating HTML coverage report..."
	@$(GO) tool cover -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)
	@echo "Coverage report: $(COVERAGE_HTML)"

.PHONY: lint
lint: require-golangci-lint
	@echo "Checking for forbidden linter suppression directives..."
	@matches="$$( \
		$(GO_FILE_FIND) -exec grep -HnEi -- \
			'(//|/\*)[[:space:]]*(nolint([[:space:]:]|$$)|#nosec([[:space:]]|$$)|gosec:disable([[:space:]]|$$)|revive:disable(-line|-next-line)?([[:space:]:]|$$)|lint:(ignore|file-ignore)([[:space:]]|$$)|exhaustive:ignore(-default-case-required)?([[:space:]]|$$))' \
			{} + || true \
	)"; \
	if [[ -n "$$matches" ]]; then \
		echo "$$matches"; \
		echo "Linter suppression directives are forbidden."; \
		exit 1; \
	fi
	@echo "Running golangci-lint..."
	@$(GOLANGCI_LINT) run --modules-download-mode=$(GOLANGCI_MODULES_DOWNLOAD_MODE) $(PKGS)

.PHONY: vuln
vuln: require-govulncheck
	@echo "Running govulncheck..."
	@if [[ -n "$(GOVULNDB)" ]]; then \
		$(GOVULNCHECK) -db "$(GOVULNDB)" $(PKGS); \
	else \
		$(GOVULNCHECK) $(PKGS); \
	fi

.PHONY: ci-check
ci-check: install-tools check

.PHONY: check
check: tools fmt-check mod-check vet build test lint vuln

.PHONY: deep-check
deep-check: check test-race coverage

.PHONY: clean
clean:
	@rm -f $(COVERAGE_FILE) $(COVERAGE_HTML)
