# Research: FTS5 search (#58)

Facts gathered before designing. Everything here was read or measured, not
assumed. Line references are against `d5968b4` (`origin/main` at session start).

## Where search lives today

Substring match over titles only, in two places that must stay consistent:

| Location | Expression |
| --- | --- |
| `internal/database/item_repository.go:442` (in `buildItemsQuery`, line 411; backs the CLI) | `instr(lower(i.title), lower(?)) > 0` |
| `internal/database/pagination.go:175` (in `itemPageConditions`, line 155; backs the API) | same |

Surfaces:

- CLI: `feedspool items --search` — flag at `cmd/items.go:54`, wired at
  `cmd/items.go:88`.
- API: `GET /api/v1/items?q=` — `paramQuery` at `internal/api/names.go:14`,
  wired at `internal/api/items.go:95`.

`--sort` on the CLI is Go-side only: `cmd/items.go:101-102` reverses the slice
for `oldest`. `ItemFilter` has no sort field. The API validates `sort` at
`internal/api/items.go:167-174` (inside `parseItemFilters`, line 142) and turns it into `ItemPage.Ascending`.

## Item write paths

Every path that can change or remove indexed text:

| Path | Location |
| --- | --- |
| `UpsertItem` | `internal/database/item_repository.go:29-59` — `INSERT ... ON CONFLICT(feed_url, guid) DO UPDATE`; the only caller is `internal/fetcher/fetcher.go:337` |
| `MarkItemsArchived` | `item_repository.go:170-206` — sets `archived = 1`; does not touch text |
| `DeleteArchivedItems` | `item_repository.go:208-221` |
| `deleteArchivedItemsForFeed` | `item_repository.go:252-310` |
| `DeleteFeed` | `feed_repository.go:311` — `DELETE FROM feeds`, which reaches `items` only by cascade |

`items` declares `FOREIGN KEY (feed_url) REFERENCES feeds(url) ON DELETE
CASCADE` (`internal/database/schema.sql:32`), and `internal/database/db.go:55`
sets `PRAGMA foreign_keys = ON`. So `DeleteFeed` removes items without passing
through any item write path — the case that decides triggers vs. explicit
maintenance.

## Connection setup

`internal/database/db.go`: `sql.Open("sqlite", ...)` (line 43),
`SetMaxOpenConns(1)` (47), `foreign_keys = ON` (55), WAL (85),
`synchronous = NORMAL` (72), `busy_timeout = 5000` (25). `recursive_triggers`
is never set, so it is at its default (off).

## Migrations

`maxMigrationVersion` is `10` (`internal/database/migrations.go:21`). Simple
migrations are SQL strings in `getMigrations()`; ones needing Go run through the
`applySpecificMigration` switch (line 156). Migrations 4 and 9 already rewrite
every row in `items`, so a full-corpus pass is an established cost here.

Relevant existing indexes: `idx_items_effective_date` and
`idx_items_feed_effective_date`, both over
`julianday(COALESCE(published_date, first_seen))` (migration 9).

## Dependencies

`golang.org/x/net` and `github.com/PuerkitoBio/goquery` are already direct
dependencies (`go.mod`). HTML handling precedent: `internal/scraper/scraper.go`,
`internal/unfurl/unfurl.go`. No new dependency is needed.

## FTS5 availability under `modernc.org/sqlite`

Verified empirically on 2026-08-26 (recorded in
`docs/dev-sessions/2026-08-25-1752-web-api/handoff-58-fts5-search.md`), through
`database.New` rather than a bare `sql.Open`:

- SQLite 3.53.3; `PRAGMA compile_options` reports `ENABLE_FTS5`.
- Term, phrase, boolean and prefix queries work; `bm25()` and `snippet()` work;
  `porter` and `unicode61` tokenizers work.
- External-content tables work, including `('rebuild')`.

Measured over 25k items with article-sized bodies (~82 MB of text):

| Operation | Time |
| --- | --- |
| Full-corpus backfill | 3.6 s |
| Single incremental insert | ~0.5 ms |
| Ranked top-20 `bm25()` | 73 ms |
| Count of a common term | 3.5 ms |
| Prefix query (`secur*`) | 86 ms |

Latencies are worst-case: the synthetic corpus is Zipfian, so the queried term
appears in nearly every document.

Index size over the same corpus (15k-item subset):

| Layout | Size | vs. source text |
| --- | --- | --- |
| Plain `fts5` | 47.1 MB | 216% |
| External-content `fts5` | 17.7 MB | 81% |

## Probe: do triggers fire on cascade deletes?

Two throwaway probes run this session through `database.New` with the project's
real pragmas, then deleted.

**Probe 1 — one level.** An `AFTER DELETE` trigger on `items` issuing the FTS5
`'delete'` command **does** fire when the row is removed by `ON DELETE CASCADE`
from `feeds`, with `recursive_triggers` at its default. `('integrity-check')`
passes afterward. Confirmed identically under the `sqlite3` CLI.

**Probe 2 — the actual design, at cascade depth 2.** The chosen layout puts the
triggers on `item_text`, which `DeleteFeed` reaches through *two* cascades
(`feeds` → `items` → `item_text`), so probe 1 did not actually cover it.
Verified against the real proposed schema — `item_text` plus an
external-content `items_fts` with insert/update/delete triggers:

- One-level cascade (`DELETE FROM items`) clears `item_text` and the index.
- **Two-level cascade (`DELETE FROM feeds`) clears both as well** — the trigger
  fires at depth 2.
- The update trigger correctly retires stale body terms while leaving untouched
  title terms indexed.
- `bm25(items_fts, 10.0, 4.0, 1.0)` with column weights works.
- `('rebuild')` works against `content='item_text'`, and `('integrity-check')`
  passes after every step.

This is the load-bearing fact behind the trigger-based maintenance decision: it
means the purge and feed-delete paths need no FTS code at all.

## Release state of the v1 API

`v1.0.2` was tagged 2026-08-25. The API landed on `main` in `406185c` on
2026-08-26, after it. So `?q=` exists only in the `latest` dev prerelease and
has never shipped in a stable release.

## Coordination

#60 (`buildItemsQuery` / `GetItems` rewrite) merged as `d5968b4` before this
session started. The handoff's warning about editing that function in parallel
no longer applies.
