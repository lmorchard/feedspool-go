# Full-Text Search Spec

**Goal:** Give `feedspool items --search` and `GET /api/v1/items?q=` real
full-text search — matching bodies and summaries, ranked by relevance, over an
index instead of a table scan — and leave behind the shared text substrate that
#30 will build on.

**Source:** https://github.com/lmorchard/feedspool-go/issues/58

## Current state

Search is a case-insensitive substring match over `items.title` only, spelled
identically in two places (`item_repository.go:442`, `pagination.go:175`) so the
CLI and API cannot drift. It matches no body text, cannot rank, and scans every
row. See `research.md` for the full map of surfaces and write paths.

Three facts from `research.md` shape everything below:

1. FTS5 is compiled into `modernc.org/sqlite` under `CGO_ENABLED=0`, including
   external-content tables and `bm25()`. Measured, not assumed.
2. An FTS5 delete trigger on the indexed table **does** fire when rows go away
   via `ON DELETE CASCADE`. This is what makes trigger-based maintenance safe
   given that `DeleteFeed` removes items without touching any item write path.
3. `?q=` has never shipped in a tagged release — the API merged after `v1.0.2`.

## Desired end state

`q` and `--search` both take a small query language and match title, summary,
and body:

| Input | Meaning |
| --- | --- |
| `rust release` | both terms (implicit AND) |
| `"release notes"` | phrase |
| `-draft` | exclude |
| `secur*` | prefix |

Everything else is a literal term, so `C++`, `foo:bar`, and `NEAR` are text
rather than FTS5 syntax. A query of nothing but exclusions is a `400` /
non-zero exit — FTS5 cannot answer "everything except X", and returning zero
rows would read as a bug.

`sort=relevance` (API) and `--sort relevance` (CLI) join `newest`/`oldest`, and
are an error without a query. **`newest` stays the default even when a query is
present.** Search composes with every existing filter — `feed_id`, `since`,
`seen`, `archived` — as one more `WHERE` condition, not a separate query path.

A new `feedspool reindex [--force]` rebuilds the derived text and index.

## Design decisions

- **Decision:** `q` and `--search` become FTS5 outright. No `match=fts` opt-in,
  no second parameter.
  - **Why:** the substring version exists only in an untagged dev build, so
    this is finishing v1 before it ships rather than breaking it. The v1
    additive-only rule is about released contracts.
  - **Rejected:** an opt-in `match=fts` parameter, and a separate `fts=`
    parameter. Both leave two search implementations to maintain forever, a bad
    default nobody discovers, and a matching CLI flag needed to keep the
    surfaces in step.

- **Decision:** index a canonical HTML-stripped derivation, not the raw columns.
  - **Why:** raw columns make markup and href fragments searchable tokens, so
    `div` matches everything and `snippet()` would emit tag soup. The
    derivation is also the one text both #58 and #30 must agree on; deriving it
    twice is how they drift.
  - **Rejected:** external content over `items` directly — simplest by far, but
    `content='items'` means FTS5 reads column values back out of `items`, so
    the indexed text has to *be* the stored text. A Go-side transform and an
    external-content table over `items` are mutually exclusive.

- **Decision:** one `item_text` table carrying both the derived text and the
  `(generator, generator_version, source_hash, computed_at)` bookkeeping.
  - **Why:** it is the tuple both issues asked for, and #30 adds
    `item_embedding` with the same four columns. The reusable code is a Go
    backfill runner over a small interface, which #30 implements for embeddings.
  - **Rejected:** a generic `item_derived_state` table addressing arbitrary
    generators. Machinery neither feature needs yet; a working pattern plus a
    shared runner is the cheaper way to prevent drift.

- **Decision:** `title`, `summary`, `body` stay separate columns.
  - **Why:** `bm25()` column weights are most of the difference between useful
    and useless ranking. Concatenating throws that away.

- **Decision:** triggers on `item_text` maintain `items_fts`; Go maintains
  `item_text`.
  - **Why:** the split follows from what each side can do. Stripping HTML is Go
    code, so inserts and updates must be Go. Deletes carry no transform, and
    the probe shows triggers reach the cascade path that Go code cannot see —
    so purge, archive-purge, and feed-delete need no FTS code at all.
  - **Rejected:** all-explicit Go maintenance. `DeleteFeed` would silently
    leave stale index rows, and an external-content table fails silently rather
    than loudly.

- **Decision:** truncation length is a parameter of the derive function, not a
  constant. FTS passes a generous cap (a bloat guard only).
  - **Why:** #30 needs a far smaller cap for embedding token limits. A baked-in
    number is exactly how two features drift while appearing to share code.

- **Decision:** `sort=relevance` paginates by offset carried inside the opaque
  cursor; date sorts keep the existing keyset.
  - **Why:** the cursor is already documented as opaque, so this is invisible
    to clients. Worst case is the ordinary offset caveat — a repeated or skipped
    row if the index changes mid-pagination.
  - **Rejected:** a bm25 score in the cursor. bm25 depends on corpus statistics,
    so any write shifts every score and the float-equality tiebreak then
    *skips* rows rather than merely repeating one — a worse failure for a
    keyset whose whole promise is totality.

- **Decision:** `newest` stays the default sort even when a query is present.
  - **Why:** a feed reader's search is usually "what's new about X", and it
    keeps the default path on the keyset cursor rather than quietly moving every
    search onto offset pagination.

- **Decision:** migration 11 creates the schema and runs the backfill in
  committed batches with progress logging.
  - **Why:** resumable by construction, since "stale" is a query against
    `item_text`. Migrations 4 and 9 already rewrite every item row, so a
    one-time full pass is an established cost. Deferring the backfill to
    `reindex` would mean search silently returns nothing until someone runs it.

- **Decision:** strip HTML with `x/net/html`'s streaming tokenizer, not goquery.
  - **Why:** goquery builds a full document tree per item; this runs over the
    whole corpus inside a migration.

## Schema

```sql
CREATE TABLE item_text (
    item_id           INTEGER PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    title             TEXT NOT NULL DEFAULT '',
    summary           TEXT NOT NULL DEFAULT '',
    body              TEXT NOT NULL DEFAULT '',
    source_hash       TEXT NOT NULL,
    generator         TEXT NOT NULL,
    generator_version INTEGER NOT NULL,
    computed_at       DATETIME NOT NULL
);

CREATE VIRTUAL TABLE items_fts USING fts5(
    title, summary, body,
    content='item_text', content_rowid='item_id',
    tokenize="porter unicode61 remove_diacritics 2"
);
```

Plus `AFTER INSERT` / `AFTER UPDATE` / `AFTER DELETE` triggers on `item_text`
issuing the standard FTS5 external-content maintenance commands.

Ranking is `bm25(items_fts, 10.0, 4.0, 1.0)` over title, summary, body, ascending
(bm25 is negative-better), tie-broken by `effective_date DESC, i.id DESC` so the
ordering is total.

## Patterns to follow

- Migration with Go logic: the `applySpecificMigration` switch in
  `internal/database/migrations.go` (see `applyMigration4WithBackfill` at `migrations.go:252`, `applyMigration9` at `migrations.go:402`
  for the batched-rewrite shape).
- Query building: `buildItemsQuery` (`item_repository.go:411`) and
  `itemPageConditions` (`pagination.go:155`). Search is one more condition in
  each; the shared SQL fragment must be spelled once, the way the current
  `instr()` duplication is guarded by a comment at `pagination.go:174`.
- API parameter validation: `parseItemFilters` (`internal/api/items.go:142`)
  and the helpers in `internal/api/params.go`.
- HTML handling precedent: `internal/scraper/scraper.go`.
- Cursor encode/decode: `internal/api/cursor.go`.

## What we're NOT doing

- **`include=snippet`.** `snippet()` works for free with an external-content
  table, but nothing in feedspool renders search results yet, and it is purely
  additive whenever it is wanted.
- **Exposing a relevance score in the item DTO.** Same reasoning.
- **Anything from #30.** No embeddings, no clustering, no provider config. This
  spec leaves a substrate; it does not consume it.
- **`NEAR`, column filters (`title:foo`), or `OR` in the query language.** The
  parser treats them as literal text. Adding operators later is additive.
- **Changing `GetItems`' filter or sort semantics beyond adding search and
  relevance.** #60 just rewrote that function; this is not a second pass at it.
- **Touching `MarkItemsArchived` or the purge paths.** Triggers cover them.
- **A `total` or result-count field on search responses.** Same reasoning as the
  v1 spec: a second scan, wrong by the time the client reads it.

## Open questions

- **How aggressive should the FTS truncation cap be?** Default: 256 KiB of
  stripped body, as a bloat guard rather than a recall limit — long-form
  articles run well under it. Revisit only if the smoke test against a real
  database shows items hitting the cap.
- **Should `porter` stemming be on?** Default: yes. It helps recall
  ("networking" finds "network") at a small cost to exact known-item lookup.
  `generator_version` exists precisely so this can be changed later with a
  forced reindex, so it is a reversible call rather than a load-bearing one.
