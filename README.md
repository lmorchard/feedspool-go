# feedspool

A CLI tool for managing RSS/Atom feeds with SQLite storage and static website generation.

## Features

I wanted a simple tool that takes OPML & text lists of feeds, fetches those feeds
periodically into a SQLite database, and produces static HTML as a report.

I don't want an inbox of unread items like a to-do list. I want to scroll 
through a personal newspaper of recent content from the web - I stop reading
when I see stuff I saw before. This basically does that.

I can also build other tools atop the SQLite database, so this serves as a basic
foundation for other things.

Further feature highlights:

- Feed fetching from single URLs, OPML files, or text lists
- Feed subscription management (subscribe/unsubscribe)
- External OPML and text lists are the source of truth for feed subscriptions
- Database feeds are treated as ephemeral/cache
- Concurrent feed fetching with conditional HTTP (304 Not Modified)
- URL metadata extraction (unfurling) with OpenGraph, Twitter Cards, and favicons
- Parallel unfurling during feed fetching for enhanced content presentation
- Static HTML site generation with responsive design, dark mode, and rich metadata
- RSS/Atom feed autodiscovery from HTML pages
- Export database feeds to OPML or text formats
- SQLite database storage with feed history
- Multiple output formats (table, JSON, CSV)
- Automatic archival of removed items and feed list cleanup
- Configurable via YAML files with default feed list support

## Why feed "spool"?

The name is vaguely inspired by Usenet "spool" storage. [From Wikipedia](https://en.wikipedia.org/wiki/Spooling#Other_applications):

> Some store and forward messaging systems, such as uucp, used "spool" to refer to their inbound and outbound message queues, and this terminology is still found in the documentation for email and Usenet software. 

## Installation

### Download pre-built binaries

Download the latest release from the [GitHub Releases page](../../releases) for your platform:

- Linux (amd64, arm64)
- macOS (amd64, arm64) 
- Windows (amd64)

### Build from source

Use the Makefile:

```bash
make build
```

## Documentation

- **[MANUAL.md](MANUAL.md)** — operator's manual: every subcommand and flag, full configuration reference, SQLite data model, SQL and workflow recipes, behavior gotchas, Docker reference. Read this first.
- **[CLAUDE.md](CLAUDE.md)** — development notes for contributors and AI assistants.

## Quick start

```bash
feedspool init
echo "https://example.com/feed.xml" > feeds.txt
feedspool fetch --format text --filename feeds.txt
feedspool render --feeds feeds.txt --format text
feedspool serve   # http://localhost:8080
```

`feedspool --help` and `feedspool <subcommand> --help` show inline reference.
For everything beyond the basics, see [MANUAL.md](MANUAL.md).

## Docker

```bash
mkdir feedspool-data
echo "https://feeds.bbci.co.uk/news/rss.xml" > feedspool-data/feeds.txt
docker run -d -p 8889:8889 -v ./feedspool-data:/data lmorchard/feedspool:latest
```

The container auto-detects `feeds.txt` or `feeds.opml`, generates a default
config, initializes the database, fetches and renders, then runs feedspool
every 30 minutes via cron and serves the result on port 8889.

For environment variables, docker-compose, manual operations, and the two
Dockerfile variants, see [MANUAL.md#docker-reference](MANUAL.md#docker-reference).

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

This project maintains code quality through:

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
- [ ] switchable named theme directories
- [ ] Merge OPML / text lists of feeds with de-dupe
- [ ] support feed tags and/or folders?
- [ ] implement a simple REST API server to access feeds data
- [ ] add per feed fetch history log table - e.g. to detect failed feeds that should be removed
- [ ] Support using a feed list at a URL - e.g. might be cool to source a feed list from linkding or such
- [ ] add file watcher to rebuild and re-render site on changes to templates or assets?
- [ ] add enclosure media URL player - e.g. for podcasts
