# feedspool Manual

This is the operator's manual for `feedspool`. It covers every subcommand, the
configuration file, the SQLite data model, common recipes, and behavioral
gotchas. It is written for both human operators and LLM agents that need to
drive the tool.

For a project overview and installation, see [README.md](README.md).
For development workflow (build, lint, test), see [CLAUDE.md](CLAUDE.md).

## Table of Contents

- [Mental Model](#mental-model)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [Global Flags](#global-flags)
- [Subcommand Reference](#subcommand-reference)
- [Data Model](#data-model)
- [SQL Recipes](#sql-recipes)
- [Workflow Recipes](#workflow-recipes)
- [Behavior Notes and Gotchas](#behavior-notes-and-gotchas)
- [Docker Reference](#docker-reference)
- [Custom Templates and Assets](#custom-templates-and-assets)
- [Exit Codes](#exit-codes)

## Mental Model

feedspool has three layers, and most confusion comes from conflating them:

1. **Subscription list** — an OPML or text file on disk. This is the *source
   of truth* for which feeds you care about. `subscribe`, `unsubscribe`, and
   `export` operate on it. `fetch` and `purge` can be told to operate
   *relative to* it.
2. **Database** (`feeds.db`) — a SQLite spool. Feed metadata, items, and
   unfurl metadata accumulate here as you fetch. Treat it as cache: any feed
   in the DB but not in your subscription list is leftover and a candidate
   for `purge`.
3. **Rendered site** — static HTML produced from the database by `render`.
   It can be regenerated from the DB at any time and is served by `serve` (or
   any static web server).

The flow is: subscription list → `fetch` → database → `render` → static
site → `serve`.

## Quick Start

```bash
feedspool init                                    # create feeds.db
echo "https://example.com/feed.xml" > feeds.txt   # subscribe
feedspool fetch --format text --filename feeds.txt
feedspool render --feeds feeds.txt --format text
feedspool serve                                   # http://localhost:8080
```

With a `feedspool.yaml` configuring `feedlist.format` and `feedlist.filename`,
the `--format`/`--filename`/`--feeds` flags become optional.

## Configuration

feedspool reads `./feedspool.yaml` by default. Override with `--config <path>`.
A missing file is silently ignored unless `--config` was specified.

Environment variables matching Viper's mapping rules are also honored.

Precedence (highest wins): CLI flag > environment variable > config file >
built-in default.

### Full configuration reference

```yaml
# Top-level
database: ./feeds.db        # Database file path
timeout: 30s                # Per-feed/URL fetch timeout
verbose: false              # Info-level logging
debug: false                # Debug-level logging
json: false                 # Default to JSON output

# Default feed list — used by subscribe, unsubscribe, fetch, purge, render
# when --format/--filename/--feeds are not provided
feedlist:
  format: ""                # "opml" or "text"
  filename: ""              # path to feed list file

fetch:
  with_unfurl: false        # Run unfurl in parallel with fetch
  concurrency: 32           # Max concurrent feed fetches
  max_items: 100            # Max items kept per feed

render:
  output_dir: ./build
  templates_dir: ""         # "" = use embedded templates
  assets_dir: ""            # "" = use embedded assets
  default_max_age: 24h      # Time window for items
  default_clean: false      # Wipe output dir before render
  default_min_items_per_feed: 5
  default_max_items_per_feed: 50
  feeds_per_page: 25        # 0 disables pagination

serve:
  port: 8080
  dir: ./build

init:
  templates_dir: ./templates
  assets_dir: ./assets

unfurl:
  skip_robots: false        # If true, ignore robots.txt
  retry_after: 1h           # Re-attempt previously failed URLs after this
  concurrency: 32

purge:
  max_age: 30d
  skip_vacuum: false        # If true, skip VACUUM after purge
  min_items_keep: 10        # Keep at least N items per feed regardless of age
```

Note: `serve.port: 8080` is the bare CLI default. The Docker image ships with
a generated config that uses port `8889` instead; see
[Docker Reference](#docker-reference).

## Global Flags

These apply to every subcommand:

| Flag | Short | Default | Description |
|---|---|---|---|
| `--config` | `-c` | (search `./feedspool.yaml`) | Config file path |
| `--database` | `-d` | `./feeds.db` | SQLite database path |
| `--verbose` | `-v` | false | Info-level logging |
| `--debug` | | false | Debug-level logging |
| `--json` | | false | Emit JSON instead of human text |

## Subcommand Reference

### init

Create or upgrade the database, and optionally extract embedded templates and
assets to disk.

**Usage:** `feedspool init [flags]`

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `--upgrade` | false | Apply pending schema migrations to an existing DB |
| `--extract-templates` | false | Write embedded templates to `--templates-dir` |
| `--extract-assets` | false | Write embedded static assets to `--assets-dir` |
| `--templates-dir` | `./templates` | Target dir for template extraction |
| `--assets-dir` | `./assets` | Target dir for asset extraction |

**Side effects:** Creates `feeds.db` if missing; runs migrations; writes
files when `--extract-*` flags are used.

**Example:**

```bash
feedspool init
feedspool init --upgrade
feedspool init --extract-templates --extract-assets
```

### subscribe

Add a feed URL to your subscription list. Optionally autodiscover feed URLs
from a webpage.

**Usage:** `feedspool subscribe <url> [flags]`

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `--format` | (config) | `opml` or `text` |
| `--filename` | (config) | Path to subscription file |
| `--discover` | false | Treat URL as a webpage; parse its HTML for `<link>` feed references |

**Side effects:** Creates the subscription file if it does not exist; appends
the URL. Network request only when `--discover` is set. Does not touch the
database.

**Examples:**

```bash
feedspool subscribe https://example.com/feed.xml
feedspool subscribe --discover https://example.com/blog
feedspool subscribe --format opml --filename feeds.opml https://example.com/feed.xml
```

### unsubscribe

Remove a feed URL from a subscription list. Does *not* delete items from the
database — use `purge --format <fmt> --filename <file>` for that.

**Usage:** `feedspool unsubscribe <url> [flags]`

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `--format` | (config) | `opml` or `text` |
| `--filename` | (config) | Path to subscription file |

### fetch

Fetch feed content. Has three modes depending on arguments.

**Usage:** `feedspool fetch [url] [flags]`

**Modes:**

- **Single URL:** `feedspool fetch https://example.com/feed.xml`
- **From subscription file:** `feedspool fetch --format opml --filename feeds.opml`
- **Refresh DB:** `feedspool fetch` — refetches every feed currently in the database

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `--timeout` | `30s` | Per-feed HTTP timeout |
| `--max-items` | `100` | Max items kept per feed |
| `--force` | false | Ignore stored ETag/Last-Modified; refetch even if 304 would be served |
| `--concurrency` | `32` | Max concurrent fetches |
| `--max-age` | `0` | Skip feeds last fetched within this duration |
| `--remove-missing` | false | (file mode) Delete DB feeds that are not in the subscription file |
| `--format` | (config) | `opml` or `text` (file mode) |
| `--filename` | (config) | Subscription file path (file mode) |
| `--with-unfurl` | (config) | Run unfurl in parallel with the fetch |

**Side effects:** Writes feeds and items to the database. Marks items no
longer in the live feed as archived. May delete feed rows when
`--remove-missing` is used. If `--with-unfurl` is set, also writes
`url_metadata`.

**JSON output (`--json`):**

```json
{
  "mode": "single|file|database",
  "totalFeeds": 12,
  "successful": 10,
  "errors": 1,
  "cached": 1,
  "totalItems": 250,
  "removedFeeds": 0
}
```

### show

List items for a single feed.

**Usage:** `feedspool show <url> [flags]`

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `--format` | `table` | `table`, `json`, or `csv` |
| `--sort` | `newest` | `newest` or `oldest` |
| `--limit` | `0` | Max items (0 = all) |
| `--since` | (none) | Filter items after this RFC3339 timestamp |
| `--until` | (none) | Filter items before this RFC3339 timestamp |

**JSON shape:**

```json
{
  "url": "https://example.com/feed.xml",
  "title": "Example Feed",
  "description": "...",
  "Items": [
    {
      "id": 1,
      "feed_url": "https://example.com/feed.xml",
      "guid": "...",
      "title": "...",
      "link": "https://example.com/article",
      "published_date": "2026-05-01T12:00:00Z",
      "first_seen": "2026-05-01T12:05:00Z",
      "content": "...",
      "summary": "...",
      "archived": false
    }
  ]
}
```

**Side effects:** Read-only.

### unfurl

Extract OpenGraph, Twitter Card, and favicon metadata from URLs.

**Usage:** `feedspool unfurl [url] [flags]`

**Modes:**

- **Single URL:** `feedspool unfurl https://example.com/article`
- **Batch:** `feedspool unfurl` — processes item URLs in the database that
  do not yet have metadata (or whose previous fetch failed and is eligible
  for retry).

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `--limit` | `0` | Max URLs to process in batch mode (0 = no limit) |
| `--format` | (none) | Single-URL mode: `json` for structured output |
| `--concurrency` | `32` | Max concurrent fetches |
| `--retry-after` | `1h` | Retry previously failed URLs after this duration |
| `--retry-immediate` | false | Retry all failed URLs now, ignoring `--retry-after` |
| `--skip-robots` | false | Bypass robots.txt checks |

**Side effects:** Writes to `url_metadata`. Network requests to target URLs
and to their `robots.txt` (unless `--skip-robots`).

**JSON shape (single URL with `--format json`):**

```json
{
  "url": "https://example.com/article",
  "title": "...",
  "description": "...",
  "image_url": "https://example.com/og.jpg",
  "favicon_url": "https://example.com/favicon.ico",
  "metadata": { /* additional fields */ },
  "last_fetch_at": "2026-05-09T12:00:00Z",
  "fetch_status_code": 200,
  "fetch_error": null,
  "created_at": "2026-05-09T11:00:00Z",
  "updated_at": "2026-05-09T12:00:00Z"
}
```

### render

Generate a static HTML site from the database.

**Usage:** `feedspool render [feed-url...] [flags]`

If feed URLs are provided positionally, only those are rendered; otherwise
all feeds in the database (or in the subscription file, with `--feeds`) are
included.

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `--max-age` | (config: `24h`) | Time window for items, e.g. `24h`, `7d` |
| `--start` | (none) | RFC3339 start of explicit time range |
| `--end` | (none) | RFC3339 end of explicit time range |
| `--min-items-per-feed` | (config: `5`) | Floor on items per feed; `0` for none |
| `--max-items-per-feed` | (config: `50`) | Ceiling on items per feed; `0` for none |
| `--feeds-per-page` | (config: `25`) | Pagination size; `0` disables pagination |
| `--output` | `./build` | Output directory |
| `--templates` | (embedded) | Custom templates directory |
| `--assets` | (embedded) | Custom assets directory |
| `--feeds` | (none) | Subscription file to filter feeds by |
| `--format` | `text` | Subscription file format when `--feeds` is set |
| `--clean` | false | Wipe output directory before render |

`--max-age` and `--start`/`--end` are mutually exclusive. Custom template
and asset directories must already exist; the parent of `--output` must
exist.

**Side effects:** Writes HTML, copies assets. Read-only on the database.

### serve

Run a development HTTP server over the rendered site.

**Usage:** `feedspool serve [flags]`

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `--port` | `8080` | TCP port to listen on |
| `--dir` | `./build` | Directory to serve |

`PORT` env var overrides the config-file value but not an explicit `--port`
flag. Graceful shutdown on `SIGINT`/`SIGTERM` with a 5-second timeout.

This is intended for development — front it with a real web server in
production.

### purge

Two distinct cleanup operations, controlled by which flags you pass.

**Usage:** `feedspool purge [flags]`

**1. Age-based item purge (always runs).** Deletes archived items older than
`--age`, while keeping at least `--min-items` per feed regardless of age.
Orphaned `url_metadata` rows are deleted afterward.

**2. Feed-list cleanup (optional).** When `--format` and `--filename` are
provided (or configured as defaults), feeds whose URL is not in the
subscription list are deleted. ON DELETE CASCADE removes their items.

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `--age` | (config: `30d`) | Cutoff for archived-item deletion |
| `--min-items` | (config: `10`) | Per-feed floor; `0` for no floor |
| `--dry-run` | false | Report what would be deleted without modifying the DB |
| `--format` | (config) | Subscription file format for feed cleanup |
| `--filename` | (config) | Subscription file path for feed cleanup |
| `--no-vacuum` | false | Skip post-purge `VACUUM` |

**Side effects:** Deletes from `items`, `feeds`, and `url_metadata`. Runs
`VACUUM` unless suppressed or in dry-run.

**JSON shape (age-based):**

```json
{
  "mode": "age",
  "dryRun": false,
  "cutoffDate": "2026-04-09T00:00:00Z",
  "minItemsKeep": 10,
  "deleted": 412,
  "metadataDeleted": 27
}
```

**JSON shape (feed-list cleanup):**

```json
{
  "mode": "feedlist",
  "dryRun": false,
  "filename": "feeds.opml",
  "format": "opml",
  "deleted": 3,
  "metadataDeleted": 5
}
```

### export

Write all feeds currently in the database to a subscription file.

**Usage:** `feedspool export <filename> --format <opml|text>`

**Side effects:** Overwrites the target file.

### version

Print version metadata.

**Usage:** `feedspool version` (or `feedspool --version`)

**Text output:**

```
feedspool version v1.2.3
  commit: abc1234
  built:  2026-05-09T12:00:00Z
```

**JSON output (`--json`):**

```json
{ "version": "v1.2.3", "commit": "abc1234", "date": "2026-05-09T12:00:00Z" }
```

## Data Model

The database is plain SQLite. You can query it directly with `sqlite3
feeds.db` while feedspool is running (SQLite supports concurrent readers).

### `feeds`

One row per feed URL.

| Column | Type | Notes |
|---|---|---|
| `url` | TEXT PK | Feed URL |
| `title` | TEXT | Parsed feed title |
| `description` | TEXT | Parsed feed description |
| `last_updated` | DATETIME | From feed's UpdatedParsed/PublishedParsed |
| `etag` | TEXT | HTTP ETag for conditional GET |
| `last_modified` | TEXT | HTTP Last-Modified for conditional GET |
| `last_fetch_time` | DATETIME | Last fetch attempt (success or failure) |
| `last_successful_fetch` | DATETIME | Last 200 OK |
| `error_count` | INTEGER | Consecutive errors; reset on success |
| `last_error` | TEXT | Last error message |
| `latest_item_date` | DATETIME | Most recent item's clamped `published_date` |
| `feed_json` | JSON | Full parsed feed structure |

### `items`

One row per (feed_url, guid). Items not present in the latest fetch are
flagged `archived=1` rather than deleted.

| Column | Type | Notes |
|---|---|---|
| `id` | INTEGER PK | Autoincrement |
| `feed_url` | TEXT | FK → `feeds.url`, ON DELETE CASCADE |
| `guid` | TEXT | Normalized GUID; see [GUID dedup](#guid-deduplication) |
| `title` | TEXT | |
| `link` | TEXT | Item URL |
| `published_date` | DATETIME | *Clamped*; see [date clamping](#published-date-clamping) |
| `content` | TEXT | Full content (HTML entities decoded) |
| `summary` | TEXT | Description/summary |
| `archived` | BOOLEAN | `1` once item disappears from the live feed |
| `item_json` | JSON | Full parsed item |
| `first_seen` | DATETIME | Wall-clock time we first inserted this item |

Indexes: `idx_items_feed_url`, `idx_items_published_date`,
`idx_items_archived`. UNIQUE constraint on `(feed_url, guid)`.

### `url_metadata`

Unfurl results, keyed by item link URL.

| Column | Type | Notes |
|---|---|---|
| `url` | TEXT PK | |
| `title`, `description`, `image_url`, `favicon_url` | TEXT | Extracted |
| `metadata` | JSON | Extra fields (Twitter Card, OpenGraph extras) |
| `last_fetch_at` | DATETIME | |
| `fetch_status_code` | INTEGER | Last HTTP status |
| `fetch_error` | TEXT | Last error, if any |
| `created_at` | DATETIME | |
| `updated_at` | DATETIME | Auto-updated by trigger |

A row with `fetch_status_code` in 2xx is considered final; failures may be
retried per `--retry-after`.

### `schema_migrations`

Internal version tracking. Current version: 4.

## SQL Recipes

Run these via `sqlite3 feeds.db "..."` or any SQLite client.

**Recent items across all feeds:**

```sql
SELECT i.published_date, f.title AS feed, i.title, i.link
FROM items i JOIN feeds f ON f.url = i.feed_url
WHERE i.archived = 0
ORDER BY i.published_date DESC
LIMIT 50;
```

**Items mentioning a topic:**

```sql
SELECT i.published_date, i.title, i.link
FROM items i
WHERE i.archived = 0
  AND (i.title LIKE '%foo%' OR i.content LIKE '%foo%' OR i.summary LIKE '%foo%')
ORDER BY i.published_date DESC;
```

**Feeds that have not produced anything recently:**

```sql
SELECT url, title, latest_item_date
FROM feeds
WHERE latest_item_date < datetime('now', '-30 days')
   OR latest_item_date IS NULL
ORDER BY latest_item_date ASC NULLS FIRST;
```

**Feeds that consistently fail:**

```sql
SELECT url, error_count, last_error, last_fetch_time
FROM feeds
WHERE error_count > 3
ORDER BY error_count DESC;
```

**Items missing unfurl metadata:**

```sql
SELECT i.link
FROM items i
LEFT JOIN url_metadata m ON m.url = i.link
WHERE i.archived = 0 AND m.url IS NULL;
```

**Item count per feed:**

```sql
SELECT f.title, COUNT(i.id) AS items
FROM feeds f LEFT JOIN items i ON i.feed_url = f.url AND i.archived = 0
GROUP BY f.url
ORDER BY items DESC;
```

## Workflow Recipes

**Subscribe from a public OPML and render:**

```bash
curl -o feeds.opml https://example.com/list.opml
feedspool fetch --format opml --filename feeds.opml --with-unfurl
feedspool render --feeds feeds.opml --format opml
feedspool serve
```

**Refresh everything in the spool:**

```bash
feedspool fetch                # all feeds in DB
feedspool unfurl               # backfill missing metadata
feedspool render
```

**Find what's new since yesterday:**

```bash
feedspool render --max-age 24h --output ./build-daily
```

…or via SQL:

```sql
SELECT i.published_date, f.title, i.title, i.link
FROM items i JOIN feeds f ON f.url = i.feed_url
WHERE i.first_seen > datetime('now', '-1 day') AND i.archived = 0
ORDER BY i.first_seen DESC;
```

**Drop everything not in your subscription list, then trim old items:**

```bash
feedspool purge --format opml --filename feeds.opml --age 30d --dry-run
feedspool purge --format opml --filename feeds.opml --age 30d
```

**Rebuild from scratch:**

```bash
rm feeds.db build/
feedspool init
feedspool fetch --format opml --filename feeds.opml --with-unfurl
feedspool render
```

**Backup the spool:** `cp feeds.db feeds.db.bak` while the tool is idle, or
use `sqlite3 feeds.db ".backup feeds.db.bak"` for a hot copy.

## Behavior Notes and Gotchas

### Published-date clamping

Many feeds emit timestamps that are wrong: future-dated items, epoch zero,
or values from before 2000. feedspool clamps every item's `published_date`
into a sane range when ingesting:

- A `published_date` later than `now()` is clamped down to the item's
  `first_seen` value (or `now()` if `first_seen` is unknown). This stops
  feeds-from-the-future from sticking at the top of every render.
- A `published_date` before 2000-01-01 is clamped *up* to 2000-01-01 — this
  catches epoch-zero defaults from broken feeds.
- The unclamped value is preserved inside `item_json` if you need it.

`feeds.latest_item_date` is computed from the clamped values, so feed
ordering reflects clamped reality, not what the feed claims.

### Conditional HTTP and `--force`

On every fetch, feedspool sends `If-None-Match` (from stored ETag) and
`If-Modified-Since` (from stored Last-Modified). A `304 Not Modified`
response counts as a cache hit: items aren't re-processed and
`last_successful_fetch` is *not* updated. Pass `--force` to drop the
conditional headers and refetch unconditionally — useful when a feed's
content changed but its server lies about it.

### Subscription list is the source of truth

`subscribe` and `unsubscribe` modify the OPML/text file only — they don't
touch the database. The database accumulates whatever you fetch. To bring
the DB back in line with the subscription list, run `purge --format <fmt>
--filename <file>` (or `fetch --remove-missing`).

### What `purge` actually deletes

Age-based purge deletes *archived* items only. Live items are never
deleted by age. The `--min-items` floor protects the N most recent items
per feed regardless of age, so a feed that goes quiet doesn't lose its
entire history at once.

Feed-list cleanup deletes feeds whose URL is not in the subscription file,
along with all of their items via cascade. Run with `--dry-run` first.

### Unfurl retry semantics

A previous unfurl attempt with status 2xx is final and never retried.
Failed attempts are eligible for retry only after `--retry-after` has
elapsed since `last_fetch_at`. Use `--retry-immediate` to override that
window. By default, `robots.txt` is consulted before each fetch; pass
`--skip-robots` to bypass it.

### Concurrency and rate limiting

There is no per-host rate limiting. The only knob is `--concurrency`
(default 32 for both `fetch` and `unfurl`). Lower it if you're hammering a
small upstream.

### GUID deduplication

Some feeds (notably the BBC) emit GUIDs that change on every fetch by
appending a fragment. feedspool normalizes the GUID by stripping the
fragment and falling back to a hash of `link + title` when needed, so the
same item doesn't get re-inserted on every refresh.

### HTML entity decoding

Feed titles, descriptions, content, and summaries are unescaped on ingest
so that consumers get plain HTML, not double-encoded entities.

### Concurrent reads while running

SQLite supports multiple readers, so you can `sqlite3 feeds.db` while a
fetch is in progress. Avoid concurrent writes (e.g., two `feedspool fetch`
processes against the same DB).

## Docker Reference

The `lmorchard/feedspool` image bundles feedspool with cron and a
generated config that fetches every 30 minutes and serves on port 8889.

### Volume layout

Mount a host directory at `/data`. Inside it:

| Path | Purpose |
|---|---|
| `feeds.txt` *or* `feeds.opml` | Subscription file (auto-detected) |
| `feedspool.yaml` | Optional; auto-generated from a template if missing |
| `feeds.db` | Created automatically |
| `build/` | Rendered HTML, served by the container |

### Environment variables

| Var | Default | Effect |
|---|---|---|
| `PORT` | `8889` | HTTP server port (also exposed by `EXPOSE`) |
| `CRON_SCHEDULE` | `*/30 * * * *` | Cron expression for periodic fetch+render |

### Quick start

```bash
mkdir feedspool-data
echo "https://feeds.bbci.co.uk/news/rss.xml" > feedspool-data/feeds.txt
docker run -d -p 8889:8889 -v ./feedspool-data:/data lmorchard/feedspool:latest
```

### docker-compose

```yaml
services:
  feedspool:
    image: lmorchard/feedspool:latest
    ports:
      - "8889:8889"
    volumes:
      - ./feedspool-data:/data
    environment:
      - PORT=8889
    restart: unless-stopped
```

### One-shot operations

```bash
docker run --rm -v ./feedspool-data:/data lmorchard/feedspool:latest init
docker run --rm -v ./feedspool-data:/data lmorchard/feedspool:latest fetch
docker run --rm -v ./feedspool-data:/data lmorchard/feedspool:latest render
```

### Building images locally

- `Dockerfile` — multi-stage build from source. Slower, fully
  self-contained: `docker build -t feedspool .`
- `Dockerfile.prebuilt` — single-stage build from an existing binary,
  used by CI: `make build && docker build -f Dockerfile.prebuilt -t feedspool .`

### Troubleshooting

```bash
docker logs <name>
docker exec -it <name> /bin/sh
docker exec <name> /usr/local/bin/feedspool fetch
```

Common issues: host directory permissions, wrong path in `feedspool.yaml`,
or an empty/missing subscription file.

## Custom Templates and Assets

The default templates and CSS are embedded in the binary. Extract them and
point `render` at the copies to customize.

```bash
feedspool init --extract-templates --extract-assets
# edit ./templates/index.html and ./assets/style.css
feedspool render --templates ./templates --assets ./assets
```

The site uses HTML `<details>` for collapsible items, supports pagination
via `--feeds-per-page`, and exposes feed descriptions as tooltips.

## Exit Codes

- `0` — success
- `1` — any error (bad flags, DB error, network failure, validation, etc.)

There is currently no distinction between error classes via exit codes;
parse stderr or use `--json` for structured failure output where available.
