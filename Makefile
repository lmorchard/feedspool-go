.PHONY: build build-static test clean run lint format fmt setup print-golangci-lint-version

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.0.1")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -X github.com/lmorchard/feedspool-go/cmd.Version=$(VERSION) -X github.com/lmorchard/feedspool-go/cmd.Commit=$(COMMIT) -X github.com/lmorchard/feedspool-go/cmd.Date=$(DATE)

# Output path for `build`/`build-static`. Override to build somewhere other
# than the repo root, e.g. `make build BINARY=/tmp/feedspool-test`.
BINARY := feedspool

# Keep this in step with the version CI installs. CI reads it from here via
# `make print-golangci-lint-version`, so this is the only place to change it.
GOLANGCI_LINT_VERSION := v2.13.1

# Tools live under the repo, not in GOPATH/bin, so pinning them here cannot
# clobber a globally installed copy. The version is part of the filename, so
# bumping the pin above installs the new one instead of reusing a stale binary.
TOOLS_DIR := $(CURDIR)/bin
GOLANGCI_LINT := $(TOOLS_DIR)/golangci-lint-$(GOLANGCI_LINT_VERSION)

build:
	@echo "Building for $(shell go env GOOS)/$(shell go env GOARCH)"
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) main.go

build-static:
	@echo "Building static binary for $(shell go env GOOS)/$(shell go env GOARCH)"
	@if [ "$(shell go env GOOS)" = "linux" ]; then \
		echo "Using static linking for Linux build"; \
		go build -ldflags "$(LDFLAGS) -linkmode external -extldflags '-static'" -o $(BINARY) main.go; \
	else \
		go build -ldflags "$(LDFLAGS)" -o $(BINARY) main.go; \
	fi

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