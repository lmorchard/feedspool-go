# Implementation Plan: feedspool CLI

## Overview

This plan breaks down the implementation of a Cobra-based RSS/Atom feed management CLI into small, iterative steps. Each step builds on the previous one, ensuring incremental progress with no orphaned code.

## Phase Breakdown

### Phase 1: Foundation
1. Project setup with dependencies
2. Basic CLI structure with Cobra
3. Configuration system

### Phase 2: Database Layer
4. Database models and schema
5. Database connection and migrations
6. Basic database operations

### Phase 3: Core Commands
7. Init command implementation
8. Fetch command (single feed)
9. Show command implementation

### Phase 4: Advanced Features
10. OPML parsing
11. Concurrent feed fetching
12. HTTP caching support

### Phase 5: Polish
13. Purge command
14. Output formatting
15. Error handling and logging

---

## Implementation Prompts

### Step 1: Project Setup and Dependencies

**Context:** We're building a CLI tool called feedspool using Go, Cobra, SQLite, and gofeed for RSS parsing.

**Prompt:**
Create the initial Go project structure for feedspool. Set up go.mod with these dependencies:
- github.com/spf13/cobra for CLI framework
- github.com/spf13/viper for configuration
- github.com/mattn/go-sqlite3 for SQLite database
- github.com/mmcdole/gofeed for RSS/Atom parsing
- github.com/sirupsen/logrus for logging

Create a basic main.go that imports these packages and has a simple main function that prints "feedspool CLI". Also create a Makefile with build, test, and clean targets.

---

### Step 2: Basic Cobra CLI Structure

**Context:** Building on the project setup, we need the core CLI structure with global flags.

**Prompt:**
Create the Cobra CLI structure with:
1. A root command in cmd/root.go with global flags: --database (-d), --config (-c), --verbose (-v), --debug, --json
2. Store global flag values in a config struct
3. Initialize viper to read configuration from feedspool.yaml
4. Set up logrus logging based on verbose/debug flags
5. Update main.go to execute the root command

The root command should describe the tool as "feedspool - RSS/Atom feed management CLI".

---

### Step 3: Configuration System

**Context:** We need a robust configuration system that merges config file and CLI flags.

**Prompt:**
Enhance the configuration system:
1. Create internal/config/config.go with a Config struct containing all settings
2. Implement config file loading with defaults (database: ./feeds.db, concurrency: 32, timeout: 30s, max-items: 100)
3. Make CLI flags override config file values
4. Add a helper function to get the current config from any command
5. Create a sample feedspool.yaml.example file

---

### Step 4: Database Models and Schema

**Context:** We need SQLite database models for feeds and items with JSON columns for flexibility.

**Prompt:**
Create database models and schema:
1. Create internal/database/models.go with Feed and Item structs
2. Feed fields: URL (PK), Title, Description, LastUpdated, ETag, LastModified, LastFetchTime, LastSuccessfulFetch, ErrorCount, LastError, FeedJSON
3. Item fields: ID (PK), FeedURL (FK), GUID, Title, Link, PublishedDate, Content, Summary, Archived, ItemJSON
4. Create internal/database/schema.sql with CREATE TABLE statements including JSON columns
5. Add methods to convert gofeed types to our models, using JSON for extra fields

---

### Step 5: Database Connection and Migrations

**Context:** We need database initialization and migration support.

**Prompt:**
Implement database connection and migrations:
1. Create internal/database/db.go with Connect() function that opens SQLite connection
2. Implement InitSchema() that executes schema.sql
3. Add migration support with a migrations table tracking version
4. Create GetDB() function that returns the database connection
5. Handle database file creation if it doesn't exist
6. Add proper connection pooling and pragma settings for SQLite

---

### Step 6: Database Operations

**Context:** We need CRUD operations for feeds and items.

**Prompt:**
Create database operations in internal/database/operations.go:
1. UpsertFeed(feed *Feed) - insert or update feed
2. GetFeed(url string) - retrieve feed by URL  
3. GetAllFeeds() - list all feeds
4. UpsertItem(item *Item) - insert or update item by GUID
5. GetItemsForFeed(feedURL string, limit int, since, until time.Time) - query items with filters
6. MarkItemsArchived(feedURL string, activeGUIDs []string) - archive items not in list
7. Use prepared statements and handle JSON columns properly

---

### Step 7: Init Command Implementation

**Context:** We need the init command to create and upgrade databases.

**Prompt:**
Create cmd/init.go implementing the init command:
1. Add init command to Cobra with --upgrade flag
2. Check if database file exists
3. If exists without --upgrade, error out
4. Create database and run InitSchema()
5. If --upgrade flag, run migrations
6. Provide success/failure feedback
7. Wire the command into the root command

---

### Step 8: Fetch Command (Single Feed)

**Context:** Create a fetch command that fetches a single feed URL.

**Prompt:**
Create cmd/fetch.go implementing single feed fetching:
1. Add fetch command accepting a feed URL as argument
2. Add flags: --timeout, --max-items, --force (ignore cache)
3. Use gofeed.NewParser() to fetch and parse the feed
4. If not --force, use etag/last-modified from database for conditional fetch
5. Convert to our Feed model with JSON serialization
6. Save feed and items to database using UpsertFeed and UpsertItem
7. Mark items not in feed as archived
8. Print fetch status (success/not-modified/error)
9. Wire into root command

---

### Step 9: Show Command Implementation  

**Context:** We need to display feed items from the database.

**Prompt:**
Create cmd/show.go implementing the show command:
1. Accept feed URL as argument
2. Add flags: --format (table/json/csv), --sort (newest/oldest), --limit, --since, --until
3. Query database using GetItemsForFeed with filters
4. Implement three output formats:
   - Table: date, title, link using tabwriter
   - JSON: full item data
   - CSV: common fields with headers
5. Handle sorting and pagination
6. Wire into root command

---

### Step 10: Update Command with OPML

**Context:** Create update command that processes OPML files.

**Prompt:**
Create cmd/update.go with OPML support:
1. Create internal/opml/parser.go with ParseOPML(reader io.Reader) function
2. Extract feed URLs from OPML outline elements
3. Accept OPML file path or stdin (-) as argument
4. Add flags: --concurrency, --timeout, --max-age, --max-items, --remove-missing
5. Build list of feed URLs from OPML
6. Reuse fetch logic from fetch command (extract to shared function)
7. Process feeds sequentially for now
8. If --remove-missing, delete feeds not in OPML
9. Wire into root command

---

### Step 11: Concurrent Feed Fetching

**Context:** Add concurrency to update command for faster processing.

**Prompt:**
Enhance update command with concurrent fetching:
1. Create internal/fetcher/fetcher.go with shared fetch logic
2. Move fetch logic from fetch command to fetcher package
3. Implement worker pool pattern with channels
4. Use goroutines with semaphore for concurrency limit
5. Implement per-feed timeout using context.WithTimeout
6. Skip feeds if --max-age and recently fetched
7. Show progress as "Fetching [URL] - [Title] (x/y)"
8. Collect errors but continue processing
9. Update both fetch and update commands to use shared fetcher

---

### Step 12: HTTP Caching Support

**Context:** Implement conditional requests using ETags and Last-Modified headers.

**Prompt:**
Add HTTP caching to reduce bandwidth:
1. Create internal/fetcher/fetcher.go with FetchFeed(url, etag, lastModified string) function
2. Set If-None-Match and If-Modified-Since headers
3. Handle 304 Not Modified responses
4. Extract and store ETag and Last-Modified from responses
5. Update database with caching headers
6. Skip updating items if 304 received
7. Add metrics for cache hits

---

### Step 13: Purge Command

**Context:** Implement cleanup of old archived items.

**Prompt:**
Create cmd/purge.go implementing the purge command:
1. Add --age flag (default 30d) with duration parsing
2. Parse duration strings like "30d", "1w", "48h"
3. Delete items where archived=true and published date is older than cutoff
4. Show count of deleted items
5. Add --dry-run flag to preview what would be deleted
6. Wire into root command

---

### Step 14: Output Formatting and Item Management

**Context:** Polish output formatting and item handling.

**Prompt:**
Enhance output and item management:
1. Add --json global flag support to all commands
2. Implement proper JSON output for all commands when flag is set
3. Add --max-items support to update command
4. Keep only N most recent items per feed
5. Generate GUID from hash(link+title) when item has no GUID
6. Mark items as archived when they disappear from feed
7. Format table output with proper column alignment

---

### Step 15: Error Handling and Logging

**Context:** Add comprehensive error handling and logging.

**Prompt:**
Implement robust error handling:
1. Create internal/logger/logger.go with structured logging
2. Use log levels: Debug, Info, Warning, Error
3. Add context to errors (feed URL, error type)
4. Implement retry logic for transient network errors
5. Update feed error_count and last_error in database
6. Only exit with non-zero for fatal errors
7. Add summary statistics at end of update command

---

## Integration Notes

Each step should:
- Include comprehensive error handling
- Add appropriate logging statements
- Include basic tests where applicable
- Update any affected existing code
- Maintain backwards compatibility

The final result should be a fully functional CLI tool that can be invoked as:
```bash
feedspool init
feedspool fetch https://example.com/feed.xml
feedspool update feeds.opml
feedspool show https://example.com/feed.xml
feedspool purge --age 7d
```