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

## Usage

### Help

You can get general help and subcommand-specific help with embedded documentation:

```bash
./feedspool --help
./feedspool init --help
./feedspool version
```

### Initialize database

```bash
# Initialize database only
./feedspool init
```

### Feed Fetching

```bash
# Fetch a single feed
./feedspool fetch https://example.com/feed.xml

# Fetch all feeds from OPML file
./feedspool fetch --format opml --filename feeds.opml

# Fetch all feeds from text list
./feedspool fetch --format text --filename feeds.txt

# Fetch all feeds in database
./feedspool fetch

# Fetch feeds with parallel metadata extraction (unfurling)
./feedspool fetch --with-unfurl
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

### URL Metadata Extraction (Unfurling)

Extract OpenGraph metadata, Twitter Cards, and favicons from web pages:

```bash
# Extract metadata from a single URL
./feedspool unfurl https://example.com/article

# Extract metadata from a single URL as JSON
./feedspool unfurl https://example.com/article --format json

# Process all item URLs in database without metadata  
./feedspool unfurl

# Process with custom limits and options
./feedspool unfurl --limit 100 --concurrency 8
./feedspool unfurl --retry-immediate --skip-robots
```

### Show items on the command line

```bash
./feedspool show https://example.com/feed.xml
```

### Generate static HTML site

```bash
# Generate HTML from all feeds in database
./feedspool render

# Generate HTML with a time range of feed items
./feedspool render --max-age 24h
./feedspool render --start 2023-01-01T00:00:00Z --end 2023-12-31T23:59:59Z

# Generate HTML from specific feeds
./feedspool render https://example.com/feed.xml

# Generate HTML with custom output directory
./feedspool render --output-dir /path/to/output

# Use custom templates and assets (extract first with init command)
./feedspool render --templates ./my-templates --assets ./my-assets
```

### Cleanup Operations

```bash
# Clean up old archived items that no longer appear in feeds
./feedspool purge --age 30d

# Remove unsubscribed feeds (keep only those in feed list)
./feedspool purge --format opml --filename feeds.opml
```

## Configuration

You can set defaults for just about every command line option in a YAML
configuration file, aiming to make CLI usage simple.

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

### Fetch Configuration
```yaml
fetch:
  with_unfurl: true     # Enable parallel unfurling during fetch
  concurrency: 16       # Fetch-specific concurrency
  max_items: 50         # Max items per feed
```

### Unfurl Configuration  
```yaml
unfurl:
  skip_robots: false    # Respect robots.txt (default: true)
  retry_after: "1h"     # Retry failed fetches after duration
  concurrency: 8        # Unfurl-specific concurrency
```

### Command Options
- `--concurrency` - Max concurrent fetches (default: 32)
- `--timeout` - Per-feed timeout (default: 30s) 
- `--max-items` - Max items per feed (default: 100)
- `--force` - Ignore cache headers and fetch anyway
- `--max-age` - Skip feeds fetched within this duration
- `--with-unfurl` - Extract metadata in parallel with feed fetching
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

## HTML Site Generation

The `render` command generates a static HTML site from your feeds. The generated site includes:
- Main index page with all feeds and items
- Collapsible feed items using HTML `<details>` elements
- Feed descriptions available as tooltips on feed titles

### Customization

There's a default template and static assets embedded in the binary.
These can be extracted as files and customized:

```bash
# Extract default templates and assets to filesystem
./feedspool init --extract-templates --extract-assets

# Customize the files in ./templates/ and ./assets/ directories
# Then use your custom files:
./feedspool render --templates ./templates --assets ./assets
```

This allows you to:
- Modify the HTML template structure in `templates/index.html`
- Customize the CSS styling in `assets/style.css`
- Add your own branding, colors, and layout changes
- Create completely custom site designs while keeping the data structure

## TODO

### Future Enhancements
- [ ] more sophisticated site generation - index page, per-feed pages, time-based pagination? 
- [ ] switchable named theme directories
- [ ] Merge OPML / text lists of feeds with de-dupe
- [ ] support feed tags and/or folders?
- [ ] implement a simple REST API server to access feeds data
- [ ] add per feed fetch history log table?
