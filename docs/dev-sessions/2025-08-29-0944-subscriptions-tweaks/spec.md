# Subscription Tweaks

## Overview

Refine subscription management with a unified approach that treats external OPML and text lists as the source of truth, with the database being ephemeral. This includes better command unification, subscription management, and feed discovery.

## Core Philosophy

- External OPML and text lists are the source of truth for feed subscriptions
- Database feeds are treated as ephemeral/cache
- Commands should be consistent in their interface patterns
- Configuration should support default feed list settings for streamlined workflows

## Detailed Requirements

### 1. Unified Fetch Command

**Replace both `fetch` and `update` commands with a single unified `fetch` command**

- Remove the existing `update` command entirely (no deprecation period)
- New `fetch` command behavior:
  - **Single URL**: `feedspool fetch <url>` - fetch one feed
  - **File with format**: `feedspool fetch --format opml|text <filename>` - fetch from file
  - **No arguments**: `feedspool fetch` - fetch all feeds currently in database

**File Format Support:**
- **OPML**: Existing XML-based feed list format
- **Plain Text**: One URL per line with support for:
  - Blank lines (ignored)
  - Comment lines starting with `#` (ignored)
  - No inline comments after URLs

**Validation:**
- Bail out with error if specified format doesn't match file content
- No automatic format detection - user must specify `--format` for files

**Migrated Options from `update`:**
- `--max-age`: Only fetch feeds older than specified age
- `--max-items`: Limit items per feed  
- `--remove-missing`: Remove feeds from DB that aren't in the list
- `--concurrency`: Number of concurrent fetches
- `--timeout`: Per-feed timeout

### 2. Subscription Management Commands

**New `subscribe` command to add feeds to lists**

- Command: `feedspool subscribe [--format opml|text <filename>] [--discover] <url>`
- **Default behavior**: Add URL directly to feed list
- **With `--discover`**: Parse HTML at URL for RSS/Atom `<link>` tags per [RSS Autodiscovery Standard](https://www.rssboard.org/rss-autodiscovery), then add discovered feed URL(s)
- Adds feed URL to specified OPML or text file (or config default)
- Warning if feed URL already exists in file (don't error, just warn)
- Keep autodiscovery simple - defer complex feed hunting to later iteration

**New `unsubscribe` command to remove feeds from lists**

- Command: `feedspool unsubscribe [--format opml|text <filename>] <feed-url>`
- Removes feed URL from specified OPML or text file (or config default)
- Warning if feed URL not found in file (don't error, just warn)

### 3. Export Command

**New `export` command to export database feeds**

- Command: `feedspool export --format opml|text <filename>`
- Export ALL feeds from database (no filtering options for now)
- **OPML format**: Include all appropriate metadata (feed title, description, etc.)
- **Text format**: Simple list of URLs with optional comments

### 4. Enhanced Purge Command

**Extend existing `purge` command with feed list cleanup**

- **Existing behavior**: `feedspool purge --age 30d` (purge old archived items)
- **New behavior**: `feedspool purge --format opml|text <filename>`
- Read feeds from specified file (OPML or text format)
- Delete any feeds (and their items) from database that are NOT in the file
- This provides cleanup based on authoritative feed lists
- Same file format support as other commands (OPML with metadata, text with comments/blanks)

### 5. Configuration Defaults

**Extend YAML configuration to support default feed list settings**

- New configuration options:
  ```yaml
  feedlist:
    format: "text"        # or "opml" 
    filename: "feeds.txt" # default file path
  ```

**Command behavior with defaults:**
- Commands that normally require `--format <format> <filename>` can omit both if configured
- Command-line arguments override configuration defaults
- Examples with defaults configured:
  - `feedspool fetch` - uses default format/filename instead of database feeds
  - `feedspool subscribe <feed-url>` - adds to default file
  - `feedspool purge` - purges against default file list
  - `feedspool export` - exports to default file (with backup/overwrite consideration)

**Fallback behavior:**
- If no defaults configured and no arguments provided:
  - `fetch` falls back to database feeds (current behavior)
  - Other commands require explicit format/filename arguments

## Implementation Tasks

- [ ] Remove existing `update` command
- [ ] Extend `fetch` command with new behaviors and file format support
- [ ] Add plain text file parser with comment/blank line support
- [ ] Add autodiscovery functionality to `subscribe` command via `--discover` flag
- [ ] Implement `subscribe`/`unsubscribe` commands for OPML manipulation
- [ ] Implement `subscribe`/`unsubscribe` commands for text file manipulation
- [ ] Implement `export` command with both format outputs
- [ ] Extend `purge` command with feed list cleanup functionality
- [ ] Add configuration support for default feed list format and filename
- [ ] Update all commands to use configuration defaults when arguments omitted
- [ ] Add appropriate tests for all new functionality
- [ ] Update documentation and help text
