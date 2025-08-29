# feedspool

A CLI tool for managing RSS/Atom feeds with SQLite storage.

## Features

- Fetch and store RSS/Atom feeds from OPML files
- Concurrent feed fetching with HTTP caching
- SQLite database storage with feed history
- Multiple output formats (table, JSON, CSV)
- Automatic archival of removed items
- Configurable via YAML files

## Installation

### Download pre-built binaries

Download the latest release from the [GitHub Releases page](../../releases) for your platform:

- Linux (amd64, arm64)
- macOS (amd64, arm64) 
- Windows (amd64)

### Build from source

```bash
go build -o feedspool main.go
```

Or use the Makefile:

```bash
make build
```

## Usage

### Initialize database
```bash
./feedspool init
```

### Fetch feeds from OPML
```bash
./feedspool update feeds.opml
```

### Fetch a single feed
```bash
./feedspool fetch https://example.com/feed.xml
```

### Show items from a feed
```bash
./feedspool show https://example.com/feed.xml
```

### Clean up old archived items
```bash
./feedspool purge --age 30d
```

### Show version information
```bash
./feedspool version
```

## Configuration

Create a `feedspool.yaml` file (see `feedspool.yaml.example`) or use command-line flags:

- `--database` - Database file path (default: ./feeds.db)
- `--concurrency` - Max concurrent fetches (default: 32)
- `--timeout` - Per-feed timeout (default: 30s)
- `--max-items` - Max items per feed (default: 100)

## Development

### Prerequisites

- Go 1.21 or later
- [golangci-lint](https://golangci-lint.run/usage/install/) for linting (required for `make lint`)
- [gofumpt](https://github.com/mvdan/gofumpt) for advanced formatting (required for `make format`)

Install development tools:
```bash
# Quick setup - install all required tools
make setup

# Or install manually
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install mvdan.cc/gofumpt@latest
```

### Development Commands

```bash
# Setup development tools
make setup

# Build
make build

# Test
make test

# Format code (requires gofumpt)
make format   # or make fmt

# Lint (requires golangci-lint)
make lint

# Clean
make clean
```

### Code Quality

This project maintains high code quality through:

- **Formatting**: `gofumpt` for advanced Go formatting beyond standard `go fmt`
- **Linting**: `golangci-lint` with comprehensive configuration (`.golangci.yml`)
- **Testing**: Comprehensive test suite with integration tests
- **CI/CD**: All quality checks run in GitHub Actions to ensure consistency

**Recommended workflow:**
```bash
# Format code before committing
make format

# Run tests 
make test

# Check linting
make lint
```

## TODO

- [ ] rework fetch command to take both single feed and feed list
- [ ] rework update command to fetch all feeds in DB without a list
- [ ] support plain text list of feed URLs alongside OPML
- [ ] feed "subscription" commands to add / remove feeds from OPML & text
- [ ] commands to export feeds in DB to OPML & text
- [ ] add per feed fetch history log table
- [ ] implement a static site generator to render HTML from feeds
- [ ] implement a simple REST API server to access feeds data
- [ ] implement a simple HTTP server to serve up static site and feeds API
- [ ] command to autodiscover feed URL from HTML URL
- [ ] enhance `init` command to create database, default config, and default feed list files
- [ ] `init` can also dump static site generation templates to a directory for customization
