# HTML scraper feed type

Issue: https://github.com/lmorchard/feedspool-go/issues/38

## Goal

Allow an OPML entry to declare an HTML page as a `scrape` feed with a CSS selector. Fetching that entry should reuse feedspool's HTTP/cache path, extract stable article links, and store synthesized items through the existing item lifecycle.

## Design

- Extend feed configuration with `Type` (`rss` or `scrape`) and `ScrapeSelector`.
- Treat a missing OPML type, and legacy non-`scrape` OPML types, as `rss` for backward compatibility.
- Store scrape configuration as `type` and `scrape_selector` columns on `feeds`; OPML remains authoritative when fetched from a file or directory.
- Keep text lists URL-only, consistent with the decision made for per-feed User-Agent metadata in #37/#51. Text-list entries therefore remain RSS feeds.
- Add `subscribe --type scrape --selector '<css>'` for creating or updating scrape entries. Scrape subscriptions require OPML and a non-empty selector. `--type rss` clears an existing selector.
- Add a pure `internal/scraper.Parse(io.Reader, baseURL, selector)` parser. It resolves relative URLs, uses the matched link or nearest ancestor link, deduplicates by resolved URL, and derives titles from link text, link `title`, then matched-element text.
- Dispatch only after the shared HTTP response is obtained. RSS uses gofeed; scrape uses the new parser.
- Synthesized items use their resolved link as the stable GUID, have no publication date, and rely on `first_seen` for ordering.
- Preserve conditional requests, per-feed User-Agent overrides, max-item limits, archiving, and optional unfurling for scrape feeds.

## Compatibility and errors

- Existing databases migrate without changing existing feeds' effective RSS behavior.
- A scrape feed without a selector fails before parsing with a clear error.
- Invalid CSS selectors return a wrapped parse error and do not replace prior items.
- Duplicate entries with conflicting type or selector configuration fail before any requests, as User-Agent conflicts already do.
- Direct `fetch URL` remains RSS unless that URL already has persisted scrape configuration.

## Verification

- Unit tests for parser extraction, title fallbacks, relative resolution, deduplication, invalid selectors, and no matches.
- Feed-list and subscription tests for OPML round trips, updates, validation, and configuration conflicts.
- Migration/repository tests for new columns and exact configuration synchronization.
- Fetcher/orchestrator tests for scrape dispatch, headers/cache behavior, first-seen dates, item limits, and config synchronization.
- `make format`, `make lint`, `make test`, and `make build`.
