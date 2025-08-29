# feedspool

A CLI tool for managing RSS/Atom feeds with SQLite storage.

## Features

- Unified feed fetching from single URLs, OPML files, or text lists
- Feed subscription management (subscribe/unsubscribe)
- Export database feeds to OPML or text formats
- Concurrent feed fetching with HTTP caching
- SQLite database storage with feed history
- Multiple output formats (table, JSON, CSV)
- Automatic archival of removed items and feed list cleanup
- RSS/Atom feed autodiscovery from HTML pages
- Configurable via YAML files with default feed list support

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

### Unified Feed Fetching

```bash
# Fetch a single feed
./feedspool fetch https://example.com/feed.xml

# Fetch all feeds from OPML file
./feedspool fetch --format opml --filename feeds.opml

# Fetch all feeds from text list
./feedspool fetch --format text --filename feeds.txt

# Fetch all feeds in database
./feedspool fetch
```

### Subscription Management

```bash
# Subscribe to a feed (adds to your default feed list)
./feedspool subscribe https://example.com/feed.xml

# Subscribe with autodiscovery from HTML page
./feedspool subscribe --discover https://example.com/blog

# Unsubscribe from a feed
./feedspool unsubscribe https://example.com/feed.xml

# Export database feeds to OPML
./feedspool export --format opml feeds.opml

# Export database feeds to text list
./feedspool export --format text feeds.txt
```

### Show items from a feed
```bash
./feedspool show https://example.com/feed.xml
```

### Cleanup Operations

```bash
# Clean up old archived items
./feedspool purge --age 30d

# Remove unauthorized feeds (keep only those in feed list)
./feedspool purge --format opml --filename feeds.opml
```

### Show version information
```bash
./feedspool version
```

## Configuration

Create a `feedspool.yaml` file (see `feedspool.yaml.example`) or use command-line flags:

### Global Options
- `--database` - Database file path (default: ./feeds.db)
- `--json` - Output in JSON format

### Default Feed List Configuration
```yaml
feedlist:
  format: "opml"        # or "text"
  filename: "feeds.opml" # or "feeds.txt"
```

### Command Options
- `--concurrency` - Max concurrent fetches (default: 32)
- `--timeout` - Per-feed timeout (default: 30s)
- `--max-items` - Max items per feed (default: 100)
- `--force` - Ignore cache headers and fetch anyway
- `--max-age` - Skip feeds fetched within this duration
- `--format` - Feed list format (opml or text)
- `--filename` - Feed list filename

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

### Future Enhancements
- [ ] implement a static site generator to render HTML from feeds
- [ ] implement a simple REST API server to access feeds data
- [ ] implement a simple HTTP server to serve up static site and feeds API
- [ ] enhance `init` command to create database, default config, and default feed list files
- [ ] `init` can also dump static site generation templates to a directory for customization
- [ ] add per feed fetch history log table
