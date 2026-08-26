# Session notes

## Context

- Branch: `feat/38-html-scraper`
- Worktree: `.claude/worktrees/issue-38-html-scraper`
- Base: `main`. The branch was initially stacked on PR #53 because both issues change authoritative per-feed OPML configuration. It was later rebased onto `486e6e7` after PR #54 merged.
- Pull request: https://github.com/lmorchard/feedspool-go/pull/55
- PR #54 for issue #40 is merged.

## Decisions

- OPML carries `type` and `selector` attributes. Text lists remain URL-only/RSS-only, following the explicit option chosen for related issue #37.
- Missing and legacy non-scrape OPML types remain effective RSS feeds.
- Scraping is an explicit parser type, never an automatic fallback from a failed RSS parse.
- Scraped items intentionally have no publication date and use `first_seen`.

## Current state

Implementation and independent review are complete. PR #55 is rebased directly onto `main` and ready to amend and push.

## Implemented

- Added the pure `internal/scraper` parser with MIT attribution, selector validation, nearby-link discovery, relative URL resolution, title fallbacks, and deduplication.
- Added OPML `type` and `selector` metadata, conflict detection, subscription updates, CLI flags, and OPML export support. Text lists remain URL-only RSS lists.
- Added migration 8 and repository support for `feeds.type` and `feeds.scrape_selector`; migration 7 remains PR #54's discovery-time index.
- Added migration 9 to normalize parseable legacy item timestamps to UTC RFC3339 and create global and per-feed scraper-aware effective-date indexes.
- Added scrape dispatch after the shared HTTP/cache layer. Scraped items use resolved links as GUIDs and share max-item, first-seen, archive, and unfurl handling with RSS items.
- Store missing scraped publication dates as SQL `NULL`; item time filters, ordering, purge selection, rendering queries, and unfurl selection fall back to `first_seen`.
- Normalize stored item dates and every SQLite time boundary to UTC so offset-bearing RFC3339 filters remain chronological rather than lexical.
- Updated README and operator manual coverage.
- Moved exact link and feed/GUID item lookup into the item repository while preserving ambiguous-link CLI errors and nullable publication dates.
- Kept discovery-window queries on the discovery-time index after SQLite began preferring the new effective-date ordering index.
- Changed effective-date filtering and ordering from lexical timestamp comparisons to chronological SQLite Julian dates. Unparseable legacy dates are excluded from bounded reads and retained by destructive purges.

## Verification

- `make format` — passed.
- `make lint` — passed with 0 issues.
- `make test` — passed for all packages after final changes.
- `make build BINARY=/tmp/feedspool-pr55` — passed.
- `git diff --check` — passed.

## Review

- Independent review initially found cache invalidation, nullable-date consumers, redirect-relative links, text/OPML precedence, UTC timestamp boundaries, and an item JSON inconsistency.
- Each finding received a focused regression and fix. The final review reported no remaining Important or Minor findings and marked the branch ready to merge.
- GitHub follow-up review requested moving item lookup SQL into the repository and indexing `COALESCE(published_date, first_seen)`; both changes have focused regression coverage.
- The PR #54 merge produced four content conflicts: `cmd/item.go`, `internal/database/item_repository.go`, `internal/database/item_repository_test.go`, and `internal/database/migrations.go`. All were resolved while preserving both feature sets.
- The final independent pass found that a raw `COALESCE` index still compared mixed-offset legacy timestamps lexically and did not cover the common per-feed plan. Focused regressions now cover chronological offset handling, conservative purge behavior, migration normalization, and the composite query plan.
- The narrow re-review reported no remaining Important or Minor findings. It confirmed the global, per-feed, and discovery query plans use their intended indexes without a temporary per-feed sort.
