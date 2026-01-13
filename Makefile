.PHONY: build build-static test clean run lint format fmt setup

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.0.1")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -X github.com/lmorchard/feedspool-go/cmd.Version=$(VERSION) -X github.com/lmorchard/feedspool-go/cmd.Commit=$(COMMIT) -X github.com/lmorchard/feedspool-go/cmd.Date=$(DATE)

build:
	@echo "Building for $(shell go env GOOS)/$(shell go env GOARCH)"
	go build -ldflags "$(LDFLAGS)" -o feedspool main.go

build-static:
	@echo "Building static binary for $(shell go env GOOS)/$(shell go env GOARCH)"
	@if [ "$(shell go env GOOS)" = "linux" ]; then \
		echo "Using static linking for Linux build"; \
		go build -ldflags "$(LDFLAGS) -linkmode external -extldflags '-static'" -o feedspool main.go; \
	else \
		go build -ldflags "$(LDFLAGS)" -o feedspool main.go; \
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

lint:
	@GOPATH=$$(go env GOPATH); \
	if [ ! -f "$$GOPATH/bin/golangci-lint" ]; then \
		echo "golangci-lint not found. Please install it: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		exit 1; \
	fi
	$$(go env GOPATH)/bin/golangci-lint run --timeout=5m

setup:
	@echo "Installing development tools..."
	go install mvdan.cc/gofumpt@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "Tools installed successfully!"