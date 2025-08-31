# Unfurl Thumbnails - Session Spec

## Overview

Items from feeds come with some interesting metadata, but more can often be derived by fetching and parsing the URL itself.

We'd like top be able to dig up things like a thumbnail, OpenGraph and Twitter card metadata, and general HTML metadata.

To facilitate this, we need to add a new database table to collect per-URL metadata. Let's define a general JSON column
(like feed and item tables) to store flexible metadata as we find it. Along with that, we'd like specific columns for
title, thumbnail image, and whatever specific properties might make sense for immediate access.

We also want to add a new CLI subcommand that scans the table of feed items and performs a backfill of metadata fetches
for any feed item URL that does not yet have metadata fetched. Also, let's enhance the purge command to delete metadata
for URLs of items we delete.

There are probably packages that already support most of this in go. We will need to research and find one that would
work the best and have a compatible MIT license. For example:

- https://github.com/Doist/unfurlist



## Goals

- [ ] Add a new `url_metadata` table to store fetched metadata per unique URL
- [ ] Implement metadata fetching for OpenGraph, Twitter Cards, and favicon discovery
- [ ] Create a CLI subcommand for backfilling metadata from existing feed items
- [ ] Enhance the purge command to clean up orphaned metadata
- [ ] Update HTML rendering to display thumbnails and favicons
- [ ] Create a shared HTTP client package for both feed and metadata fetching

## Success Criteria

- [ ] Metadata table stores title, description, image, favicon, and flexible JSON data
- [ ] Backfill command can process URLs concurrently with configurable limits
- [ ] Failed fetches are tracked with status codes and retry after 1 hour
- [ ] HTML output shows 150x150 thumbnail images with proper CSS styling
- [ ] Favicons appear in feed headers
- [ ] Robots.txt is respected for all metadata fetches

## Out of Scope

- [ ] JavaScript-rendered page support
- [ ] Handling paywalls, CAPTCHAs, or geo-blocking
- [ ] Domain-level favicon deduplication
- [ ] Using metadata titles as fallback for feed item titles
- [ ] Rate limiting for fetch requests

## Technical Notes

### Database Schema
```sql
CREATE TABLE url_metadata (
    url TEXT PRIMARY KEY,
    title TEXT,
    description TEXT,
    image_url TEXT,
    favicon_url TEXT,
    metadata JSON,
    last_fetch_at TIMESTAMP,
    fetch_status_code INTEGER,
    fetch_error TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX idx_url_metadata_url ON url_metadata(url);
```

### Fetch Configuration
- User-Agent: `feedspool/1.0`
- Max response size: 100KB (metadata should be in HTML head)
- Timeout: Shared with feed fetching configuration
- Follow HTTP redirects: Yes
- Retry failed fetches after: 1 hour (configurable)
- Accept only 200 OK responses (plus redirects)

### CLI Subcommand
- Name: `feedspool unfurl` (or similar)
- Default: Process all item URLs without metadata
- Option: `--limit N` to process only N URLs per execution
- Single URL mode: `feedspool unfurl <URL>` to fetch metadata for arbitrary URL
- Option: `--format json` to output single URL result to stdout as JSON
- If metadata exists for single URL, return cached version from database
- Concurrent fetching similar to feed fetcher pattern (for batch mode)
- Respects robots.txt

### Display Requirements
- Thumbnail size: 150x150px square
- CSS: `object-fit: contain` for non-square images
- Fallback: favicon → placeholder if no og:image
- Lazy loading for performance
- Favicon in feed headers when available

### Library Research Needed
- Evaluate Doist/unfurlist and alternatives
- Requirements: MIT license compatible, Go native
- Must support OpenGraph, Twitter Cards, favicon discovery
- Should handle HTML parsing efficiently within 100KB limit
