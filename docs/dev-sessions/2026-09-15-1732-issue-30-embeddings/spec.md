# Embedding Substrate Spec (issue #30, slice A)

**Goal:** Persist per-item vector embeddings from a configurable provider, so a
later slice can cluster a day's items into topics — and so "related items"
works today as proof the vectors mean something.

**Source:** [#30](https://github.com/lmorchard/feedspool-go/issues/30), brainstormed 2026-09-15

## Current state

#58 already built the substrate this needs, and left a marker for it:
`internal/database/backfill.go` declares `DerivedBackfill` with the comment
*"#58 implements it for FTS text; #30 implements it for embeddings."*

- `internal/itemtext.Derive` is the one canonical "what text represents this
  item?" function; its output is stored in `item_text` along with
  `source_hash` / `generator` / `generator_version` / `computed_at`
  (`internal/database/schema.sql:69`).
- `database.RunBackfill` (`internal/database/backfill.go`) drives resumable
  committed batches with progress reporting.
- `internal/httpclient.Request` supports POST today (`client.go:76`).
- `database.ParseTimeWindow` / `ParseDuration` already parse `2d` / `1w` /
  RFC3339 ranges (`internal/database/time_utils.go`).

The one blocking gap: `db.go:48` sets `SetMaxOpenConns(1)`, and `RunBackfill`
calls `Recompute` *inside* its transaction. Network I/O there would hold the
single connection — and therefore the whole database — for the round trip.
See `research.md` §3.

## Desired end state

```
feedspool embed --last 2d                                   # embed a window
feedspool embed --last 2d --model qwen3-embedding:0.6b      # the bake-off
feedspool embed --since <rfc3339> --until <rfc3339>         # explicit window
feedspool embed --last 2d --force                           # ignore staleness
feedspool embed --last 2d --dry-run                         # count, call nothing
feedspool related <link> [--limit N] [--model M]            # nearest neighbours
feedspool related --feed <url> --guid <guid> [--limit N]    # disambiguated
```

`embed` reports items embedded, items skipped for missing `item_text` (with a
pointer to `reindex`), and elapsed time. `related` prints a ranked table of
neighbours with similarity scores, or JSON under the global `--json` flag,
matching how `items` handles output. Both exit non-zero on invalid flags and
conflicting filters, per the `items` precedent.

`related` selects its subject **by link**, mirroring `cmd/item.go` exactly —
including `--feed` + `--guid` for a link that matches more than one item, and
the non-zero exit with each matching feed URL and GUID named on ambiguity. It
does not introduce a new identifier form; hash IDs stay an API/manifest
concern.

## Data model

Migration 12 (`maxMigrationVersion` is currently 11) adds:

```sql
CREATE TABLE IF NOT EXISTS item_embeddings (
    item_id           INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    model_id          TEXT    NOT NULL,  -- "nomic-embed-text", "qwen3-embedding:0.6b"
    dims              INTEGER NOT NULL,  -- 768 | 1024
    vector            BLOB    NOT NULL,  -- float32 little-endian, dims*4 bytes
    source_hash       TEXT    NOT NULL,  -- copied from item_text.source_hash
    generator_version INTEGER NOT NULL,
    computed_at       DATETIME NOT NULL,
    PRIMARY KEY (item_id, model_id)
);
CREATE INDEX IF NOT EXISTS idx_item_embeddings_model
    ON item_embeddings(model_id, item_id);
```

No index for the date window — migration 9 already indexes the expression.

There is deliberately **no `generator` column** alongside `generator_version`,
unlike `item_text`. `model_id` *is* the generator identity here, and it is
already in the primary key. `internal/embed` exports a `Version` constant
playing the same role `itemtext.Version` does (`internal/itemtext/itemtext.go:23`),
bumped whenever the embedded input changes for the same input text — which
includes changing a per-model input template.

Work-selection predicate (the generator's `workCondition`):

```sql
FROM items i
JOIN item_text t             ON t.item_id = i.id
LEFT JOIN item_embeddings e  ON e.item_id = i.id AND e.model_id = ?
WHERE <aliasedEffectiveDateExpression> >= julianday(?)
  AND <aliasedEffectiveDateExpression> <= julianday(?)
  AND (e.item_id IS NULL                 -- never embedded for this model
       OR e.source_hash <> t.source_hash -- the item's text changed
       OR e.generator_version <> ?)      -- our code changed
```

`JOIN` (not `LEFT JOIN`) on `item_text` skips items with no derived text
rather than embedding empty strings; `embed` counts those and points at
`reindex`. `--force` replaces the parenthesised staleness clause with `TRUE`,
mirroring `newItemTextRebuild` (`internal/database/item_text.go:37-56`).

Two models' vectors coexist for the same item, so nomic and qwen3 can be
compared without re-embedding between runs.

## Design decisions

- **Decision:** Embeddings derive from `item_text`, not from `items` directly.
  - **Why:** Two payoffs. It makes the "one canonical item text" property
    structural rather than aspirational — FTS5 and the embedder cannot drift,
    because they read the same rows. And it makes staleness a pure SQL
    comparison against the already-maintained `item_text.source_hash`, with no
    re-hashing and no reading content blobs to decide.
  - **Rejected:** Re-deriving text in the embedder. Guarantees eventual drift
    from the search index, and makes staleness detection expensive.

- **Decision:** `PRIMARY KEY (item_id, model_id)`.
  - **Why:** A bake-off has to hold two answers for the same item at once.
    Also the two models disagree on vector length (768 vs 1024), so a single
    fixed-width row cannot serve both regardless.
  - **Rejected:** Keying on `item_id` alone, mirroring `item_text`. Correct
    there (one right answer per item), wrong here — flipping models would mean
    re-embedding the whole window on every comparison.

- **Decision:** Staleness includes a `source_hash` comparison.
  - **Why:** `item_text` deliberately omits it, because a DB trigger keeps
    that table fresh on the live write path. **Embeddings have no trigger and
    no live write path** — `embed` is the only producer. Without the hash
    comparison, a revised item keeps a stale vector forever.
  - **Rejected:** Copying `item_text`'s predicate verbatim. Silently wrong.

- **Decision:** New `RunStagedBackfill` driver; `RunBackfill` untouched.
  - **Why:** Splits the phases so network I/O provably cannot hold a
    transaction: `Compute(ctx, inputs)` receives no `*sql.Tx`, so the
    constraint is enforced by the type signature rather than by a comment.
    Leaving `RunBackfill` alone means #58's proven path cannot regress.
  - **Rejected:** Batching HTTP inside the existing `Recompute`. Does not
    work — the transaction is already open, so the connection is already
    checked out. Recorded in `research.md` §3 so it is not re-attempted.
  - **Rejected:** Raising `SetMaxOpenConns`. Touches a subsystem with lock-bug
    history (#57, #59); out of scope for this issue.

- **Decision:** Window on the existing `effectiveDateExpression` —
  `julianday(COALESCE(published_date, first_seen))`
  (`internal/database/item_repository.go:20`), using the `i.`-aliased variant
  for our joined query.
  - **Why:** "Topics of the day" means when things were published, and the
    fallback survives a catch-up fetch that would stamp a week of items with
    one `first_seen`. Safe because `fetcher.clampItemDate`
    (`internal/fetcher/fetcher.go:246`) already bounds `published_date` on both
    ends at write time. And it is **the same semantics `items --since/--until`
    already has** via `buildItemsQuery` (`item_repository.go:475`) — so this
    converges with the existing convention rather than diverging from it.
    (`cmd/items.go:33` claims those flags filter on `first_seen`; that help
    text is stale. Pre-existing doc bug, out of scope, noted for later.)
  - **Why reuse the constant, not an equivalent `COALESCE`:** migration 9
    already indexes this exact expression (`migrations.go:90-92`), and SQLite
    only uses an expression index on a *textual* match. Hand-writing an
    equivalent expression silently falls back to a full scan.
  - **Rejected:** `first_seen` alone. A catch-up fetch would collapse the
    window to "the whole backlog."
  - **Rejected:** adding a new index in migration 12. Already covered by
    `idx_items_effective_date`.

- **Decision:** Flags are `--last` / `--since` / `--until`, reusing
  `database.ParseTimeWindow`.
  - **Why:** Maps onto the existing helper exactly, and `1w` comes free.
  - **Rejected:** `--max-age`. `cmd/build.go:30` documents that it already
    means two opposite things in this codebase; a third would be worse.

- **Decision:** One transport-agnostic `Provider` over HTTP; local vs. hosted
  is configuration (base URL + optional key), not a second code path. The
  interface is exactly `ModelID() string` plus
  `Embed(ctx context.Context, texts []string) ([][]float32, error)`; `Embed`
  owns batch splitting and input templating, so callers hand it any number of
  texts.
  - **Why:** `Embed(ctx, texts) ([][]float32, error)` has no HTTP in the
    signature, so an in-process provider is a later second implementation with
    nothing to unwind.
  - **Why no `Dims()` method:** dimensionality is `len(vectors[0])` from the
    first response. A separate probe call would be a second source of truth
    that could disagree with the vectors actually returned.
  - **Rejected for now:** in-process inference via hugot + GoMLX. It *is*
    feasible cgo-free (measured: +~9 MB, 344 packages), but forces a
    256-token model that would truncate most items and confound the one open
    question this work exists to answer. Full reasoning in `research.md` §6.

- **Decision:** `vector` is float32 little-endian, exactly `dims*4` bytes,
  length-asserted on read; similarity is a plain dot product.
  - **Why:** Both models return `‖v‖ = 1.0000` (measured), so normalising or
    storing a magnitude would be dead weight. The decoder verifies the norm on
    first read per model, so the assumption cannot rot silently.
  - **Rejected:** float64 (double the space, no similarity gain) and
    quantisation (complexity bought for nothing at a few thousand vectors).

- **Decision:** `model_id` is the configured provider model name
  (`qwen3-embedding:0.6b`); input-template changes ride `generator_version`.
  - **Why:** A template change should rewrite rows, which a version bump
    already does. A/B-ing two templates for one model is not a real need.

- **Decision:** The Ollama provider sends an explicit `options.num_ctx`, and
  the per-model defaults set it to the model's true native window (8192 for
  nomic, 32768 for qwen3).
  - **Why:** Measured — Ollama's `nomic-embed-text` card caps context at 2K
    unless `num_ctx` is set, and **2.3% of a real 2-day window (44 of 1,898
    items) exceeds 2,048 tokens** (`research.md` §5). Leaving the default in
    place silently truncates the longest, most substantive items, which are
    the most topically distinctive ones. Setting 8192 drops overflow to 0.3%.
  - **Rejected:** Relying on the model's advertised native window. What the
    model supports and what Ollama configures by default are different numbers,
    and only the second one is in effect.

- **Decision:** `embed.api_key` has no command-line flag.
  - **Why:** Follows `config.APIConfig.Token`, whose comment gives the reason:
    a token on the command line lands in `ps` output.

- **Decision:** Per-model input templates live in the provider
  (`clustering: {text}` for nomic, `{text}` for qwen3, etc.).
  - **Why:** Not optional — the template is part of what produced the vector.
    This corrects the August note on #30 that treated the provider as just a
    URL and a model name. See `research.md` §2.

## Patterns to follow

- **Backfill generator shape:** mirror `internal/database/item_text.go` —
  `workCondition()` returning a WHERE fragment plus bind args, `Name()`,
  `Version()`, and the `//nolint:gosec` comment form used where a package
  constant is concatenated into SQL (`item_text.go:60-70`).
- **Date windowing:** reuse `aliasedEffectiveDateExpression` and the
  `julianday(?)` bind form from `buildItemsQuery`
  (`internal/database/item_repository.go:520-527`), including the
  `formatDatabaseTime` conversion on the bound values. Do not hand-write an
  equivalent expression — see the design decision above.
- **Migration:** follow `migrations.go` — version constant, description in the
  map, DDL entry, and a test in `migrations_test.go` (migration 11 for #58 is
  the closest model).
- **Command wiring:** follow `cmd/items.go` for flag registration, RFC3339
  parsing, `--json` handling, and non-zero exit on conflicting filters;
  `cmd/reindex.go` for a backfill-driving command with progress output.
- **Config:** follow `internal/config/config.go` — a typed sub-struct with
  `mapstructure` tags, `viper.SetDefault` in `cmd/root.go`, defaults as named
  constants.
- **Progress output:** `RunStagedBackfill` takes the same
  `progress func(done, total int64)` callback shape `RunBackfill` defines
  (`internal/database/backfill.go:33`), with the same "resumed runs count from
  zero against the smaller remainder" semantics, so `cmd/embed.go` can report
  exactly the way `cmd/reindex.go` does.
- **Testing the no-transaction invariant:** `RunStagedBackfill`'s test uses a
  fake generator whose `Compute` attempts a database read under a short
  context deadline. With `SetMaxOpenConns(1)`, an open transaction makes that
  read block until the deadline — so the test fails if the driver ever
  regresses to holding a transaction across `Compute`. This is the invariant
  the whole driver exists to protect; assert it directly rather than trusting
  it.

## What we're NOT doing

- **Clustering, `feedspool topics`, and topic labels.** Slice B. Labels need
  an LLM config surface and only matter once clusters are known to be good.
- **In-process embedding.** Deferred with reasoning recorded (`research.md` §6).
- **A vector index / ANN.** At a few thousand vectors per window a full scan is
  sub-millisecond. The `CGO_ENABLED=0` constraint never binds here.
- **Changing `SetMaxOpenConns`, `RunBackfill`, or anything FTS5.**
- **Embedding on the `fetch` path.** `embed` is explicit and separate; network
  cost should not be smuggled into a fetch.
- **Dimension truncation (Matryoshka).** Both models support it; storage at
  this scale does not justify the knob.
- **Pruning old models' vectors.** ~248 MB for a *full* 34,613-item corpus
  backfill in both models (+45% on the 551 MB spool) is acceptable; normal
  window operation is ~6 MB. Revisit only if that changes.
- **`internal/api` / HTTP surface for embeddings or `related`.** CLI only.
- **Hosted-provider polish** (rate-limit backoff, cost accounting, key
  rotation). The config accepts a base URL and key; tuning waits for a real
  hosted user.

## Smoke-test corpus (resolved)

`data/feeds-backup.db` — 551 MB, 460 feeds, **34,613 items**. **Never open the
original**; it would be migrated in place. Copy it first
(`/tmp/feedspool-issue30/spool.db` is the working copy already made and
migrated to schema 12; `item_text` backfill for all 34,613 items took 17.6 s).

Window sizes measured on it: 919 items in 1 day, 1,898 in 2 days, 2,548 in 3
days. Full measurements in `research.md` §5.

## Open questions

- **Which model wins the bake-off?** Deliberately unanswered — it is the
  experiment. **Default:** `nomic-embed-text` is the configured default
  (smallest, Apache-2.0, purpose-built `clustering:` prefix); the smoke test
  records both and the default changes only if qwen3 is visibly better.
