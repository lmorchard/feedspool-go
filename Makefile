.PHONY: build test clean run lint format fmt setup check-toolchain print-golangci-lint-version print-go-version

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

# The Go toolchain is pinned by the `toolchain` directive in go.mod, which is
# the single source of truth. Two separate mechanisms consume it, and neither
# side ends up on whatever Go happens to be on PATH:
#
#   - locally, exporting GOTOOLCHAIN here puts every `go` invocation under make
#     on the pinned version;
#   - in CI, the workflow installs this exact version, read back out through
#     `make print-go-version`.
#
# CI reads it rather than relying on actions/setup-go@v5, which resolves
# `go-version-file: go.mod` from the `go` directive and not the `toolchain`
# one. Without that step CI installs 1.25.0 and then downloads the pinned
# toolchain on top of it on every run -- correct, because GOTOOLCHAIN=auto
# honours the directive, but a download per job and a mechanism easy to
# mistake for setup-go doing the work.
#
# It has to be pinned because golangci-lint carries a type checker that only
# understands the Go releases it was built against. The pinned v2.13.1 run
# against a Go 1.27 stdlib dies inside crypto/internal/randutil with "method
# must have no type parameters" before reading a line of this repo, so `make
# lint` failed locally while CI passed on the same commit -- CI was installing
# Go from the `go` directive and happened to be old enough to work (#67).
#
# The pin has a floor as well as a ceiling, and the window is one minor
# release wide. golangci-lint v2.13.1's own go.mod requires go >= 1.26.0, so
# 1.25.x cannot build it; its type checker cannot read a 1.27 stdlib, so 1.27.x
# cannot run it. 1.26.x is the only version that does both.
#
# Moving to a newer Go therefore means bumping this together with
# GOLANGCI_LINT_VERSION to a release that understands it -- and checking the new
# linter's own go.mod floor, not just its ceiling.
GO_TOOLCHAIN := $(shell awk '/^toolchain /{print $$2}' go.mod)
export GOTOOLCHAIN = $(GO_TOOLCHAIN)

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

# Built with the pinned toolchain, not whatever is on PATH: a golangci-lint
# compiled against one stdlib cannot always type-check another.
$(GOLANGCI_LINT):
	@echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION) into $(TOOLS_DIR)"
	@GOBIN=$(TOOLS_DIR) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@mv $(TOOLS_DIR)/golangci-lint $@

# check-toolchain turns a bad pin into one readable line, instead of letting
# golangci-lint fail somewhere deep inside the standard library.
#
# The GOTOOLCHAIN export above beats anything in the environment, so the second
# check is a backstop rather than the common case. The first is the one that
# earns its keep: lose the directive from go.mod and both sides fall silently
# back to whatever Go is on PATH, which is the state #67 describes.
check-toolchain:
	@test -n "$(GO_TOOLCHAIN)" || { echo "go.mod has no 'toolchain' directive. That pin is what keeps make lint and CI on the same Go, and agreeing about findings (#67) -- restore it."; exit 1; }
	@test "$$(go env GOVERSION)" = "$(GO_TOOLCHAIN)" || { echo "Go toolchain mismatch: running $$(go env GOVERSION), go.mod pins $(GO_TOOLCHAIN) (GOTOOLCHAIN=$$(go env GOTOOLCHAIN))."; exit 1; }

lint: check-toolchain $(GOLANGCI_LINT)
	@echo "Linting with golangci-lint $(GOLANGCI_LINT_VERSION) on $(GO_TOOLCHAIN)"
	$(GOLANGCI_LINT) run --timeout=5m

print-golangci-lint-version:
	@echo $(GOLANGCI_LINT_VERSION)

print-go-version:
	@echo $(GO_TOOLCHAIN)

setup: $(GOLANGCI_LINT)
	@echo "Installing development tools..."
	go install mvdan.cc/gofumpt@latest
	@echo "Tools installed successfully!"