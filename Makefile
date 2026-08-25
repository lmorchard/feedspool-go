.PHONY: build test clean run lint format fmt setup print-golangci-lint-version

# Assigned with ?= so the release workflows can pass the authoritative values
# in the environment. They already do, deriving VERSION from the pushed tag;
# with := those exports were silently ignored, because a Makefile assignment
# beats the environment.
#
# --match keeps the fallback away from the rolling "latest" tag. That tag sits
# on the same commit as a release tag, and actions/checkout rewrites the fetched
# release tag to point straight at the commit, so in CI both are lightweight and
# describe is free to pick either one. Restricting the pattern removes the
# coin flip.
VERSION ?= $(shell git describe --tags --always --dirty --match 'v[0-9]*' 2>/dev/null || echo "v0.0.1")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -X github.com/lmorchard/feedspool-go/cmd.Version=$(VERSION) -X github.com/lmorchard/feedspool-go/cmd.Commit=$(COMMIT) -X github.com/lmorchard/feedspool-go/cmd.Date=$(DATE)

# Output path for `build`. Override to build somewhere other than the repo
# root, e.g. `make build BINARY=/tmp/feedspool-test`.
BINARY := feedspool

# The sqlite driver is pure Go (modernc.org/sqlite), so nothing here needs a C
# toolchain. Disabling cgo outright keeps that true: builds stay static, cross
# compilation needs no target toolchain, and the darwin binaries get their
# ad-hoc signature from Go's internal linker instead of an external one.
export CGO_ENABLED = 0

# Keep this in step with the version CI installs. CI reads it from here via
# `make print-golangci-lint-version`, so this is the only place to change it.
GOLANGCI_LINT_VERSION := v2.13.1

# Tools live under the repo, not in GOPATH/bin, so pinning them here cannot
# clobber a globally installed copy. The version is part of the filename, so
# bumping the pin above installs the new one instead of reusing a stale binary.
TOOLS_DIR := $(CURDIR)/bin
GOLANGCI_LINT := $(TOOLS_DIR)/golangci-lint-$(GOLANGCI_LINT_VERSION)

build:
	@echo "Building for $(shell go env GOOS)/$(shell go env GOARCH) (cgo disabled)"
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) main.go

test:
	go test ./...

clean:
	rm -f feedspool
	rm -f feeds.db

run: build
	./feedspool

format fmt:
	@GOPATH=$$(go env GOPATH); \
	if [ ! -f "$$GOPATH/bin/gofumpt" ]; then \
		echo "gofumpt not found. Please install it: go install mvdan.cc/gofumpt@latest"; \
		exit 1; \
	fi
	go fmt ./...
	$$(go env GOPATH)/bin/gofumpt -w .

# Built with the local Go toolchain on purpose: a golangci-lint compiled
# against an older stdlib cannot type-check a newer one.
$(GOLANGCI_LINT):
	@echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION) into $(TOOLS_DIR)"
	@GOBIN=$(TOOLS_DIR) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@mv $(TOOLS_DIR)/golangci-lint $@

lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run --timeout=5m

print-golangci-lint-version:
	@echo $(GOLANGCI_LINT_VERSION)

setup: $(GOLANGCI_LINT)
	@echo "Installing development tools..."
	go install mvdan.cc/gofumpt@latest
	@echo "Tools installed successfully!"