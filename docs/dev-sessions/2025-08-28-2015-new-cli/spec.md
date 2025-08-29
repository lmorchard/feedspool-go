# Dev Session Spec

## Session: 2025-08-28-2015-new-cli

### Overview

In this session we want to build a CLI tool that accepts:

- an OPML file containing a list of RSS/Atom feeds
- fetches each of the RSS/Atom feeds in the OPML file
- updates an SQLite database with details for each feed and all the itetems from each feed 

Let's rework this project as a Cobra-based CLI with multiple subcommands.

We'll initially need subcommands like:

- init - set up the database
- update - given an OPML file, poll all the feeds and update the database
- fetch - fetch a single feed URL and update the database
- show - given the URL to a feed, list items for that feed

For feed polling, we'll want to support concurrent fetching.

Also use e-tag and if-modified-since headers to support conditional 304 Not Modified fetching.

### Goals

- Build a robust CLI tool for RSS/Atom feed management
- Support concurrent feed fetching with HTTP caching
- Provide flexible querying and output options
- Maintain feed history and health metrics

### Requirements

#### Database Schema

**Feeds Table:**
- url (text, primary key)
- title (text)
- description (text)
- last_updated (timestamp)
- etag (text) - HTTP caching header
- last_modified (text) - HTTP caching header
- last_fetch_time (timestamp)
- last_successful_fetch (timestamp)
- error_count (integer)
- last_error (text)
- feed_json (json) - Full feed data as JSON using gofeed's String() method

**Items Table:**
- id (integer, primary key)
- feed_url (text, foreign key)
- guid (text) - Primary identifier, or hash of link+title if no GUID
- title (text)
- link (text)
- published_date (timestamp)
- content (text)
- summary (text)
- archived (boolean, default false) - True if item no longer in feed
- item_json (json) - Full item data as JSON

#### CLI Commands

**Global Options:**
- `--database, -d` (default: ./feeds.db) - Database file path
- `--config, -c` (default: feedspool.yaml) - Config file path
- `--verbose, -v` - Verbose output
- `--debug` - Debug output
- `--json` - JSON output format for scripting

**init Command:**
- Creates new database with schema
- Options:
  - `--upgrade` - Upgrade existing database schema
- Fails if database exists without --upgrade flag

**update Command:**
- Accepts OPML file (filename or stdin)
- Fetches all feeds concurrently
- Updates database with feed metadata and items
- Options:
  - `--concurrency` (default: 32) - Max concurrent fetches
  - `--timeout` (default: 30s) - Per-feed fetch timeout
  - `--max-age` - Skip feeds fetched within this duration
  - `--max-items` (default: 100) - Max items to keep per feed
  - `--remove-missing` - Delete feeds not in OPML
- Progress output: "Fetching [URL] - [Title] (x/y)"
- Uses etag and if-modified-since for conditional fetching
- Marks missing items as archived rather than deleting

**fetch Command:**
- Accepts a single feed URL as argument
- Fetches and updates that specific feed
- Updates database with feed metadata and items
- Options:
  - `--timeout` (default: 30s) - Feed fetch timeout
  - `--max-items` (default: 100) - Max items to keep per feed
  - `--force` - Ignore cache headers and fetch anyway
- Uses etag and if-modified-since for conditional fetching
- Marks missing items as archived rather than deleting
- Useful for testing or updating individual feeds

**show Command:**
- Lists items for a given feed URL from database
- Options:
  - `--format` (table|json|csv) - Output format
  - `--sort` (newest|oldest) - Sort order
  - `--limit` - Max items to return
  - `--since` - Filter by date range
  - `--until` - Filter by date range
- Table format: date, url, title
- CSV format: common fields with column headers
- JSON format: all fields

**purge Command:**
- Deletes archived items from database
- Options:
  - `--age` (default: 30d) - Delete items older than this

#### Error Handling

- Exit with non-zero code only for fatal errors (e.g., database connection failure)
- Log errors to stderr
- Handle malformed OPML with error
- Skip non-feed OPML entries silently
- Mark failed feeds and continue processing others

#### Configuration

- Support config file (feedspool.yaml) for default options
- Command-line options override config file
- Config file location selectable via --config flag

### Constraints

- Go-based implementation using Cobra framework
- SQLite for data storage
- gofeed library for RSS/Atom parsing
- No external service dependencies
- Single-user local tool

### Success Criteria

- Successfully fetches and stores feeds from OPML files
- Handles concurrent fetching without data corruption
- Respects HTTP caching headers to minimize bandwidth
- Provides flexible querying and output options
- Maintains feed health metrics for monitoring
- Gracefully handles errors without data loss
