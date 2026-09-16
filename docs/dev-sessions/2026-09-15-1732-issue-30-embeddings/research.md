# Research: Embeddings substrate for issue #30

Everything here was measured or read from source on 2026-09-15, not recalled.
Where a prior claim turned out to be wrong, the correction is recorded
alongside it — the wrong version is kept deliberately, so nobody re-derives it.

## 1. What #58 already built (and left for us)

`internal/database/backfill.go` defines `DerivedBackfill` with this comment:

> "#58 implements it for FTS text; #30 implements it for embeddings."

So the substrate predicted in the August notes on issue #30 exists:

| Piece | Location | Reusable? |
|---|---|---|
| Canonical item text | `internal/itemtext.Derive` | Yes, directly |
| Source hashing | `itemtext.SourceHash`, stored in `item_text.source_hash` | Yes, directly |
| Staleness bookkeeping | `item_text(generator, generator_version, computed_at)` | Yes, as a pattern |
| Resumable batched backfill | `database.RunBackfill` | **No — see §3** |

`item_text` schema (`internal/database/schema.sql:69`):

```sql
CREATE TABLE item_text (
    item_id INTEGER PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    title TEXT, summary TEXT, body TEXT,
    source_hash TEXT NOT NULL,
    generator TEXT NOT NULL,
    generator_version INTEGER NOT NULL,
    computed_at DATETIME NOT NULL
);
```

## 2. Ollama measurements (this machine, 2026-09-15)

Ollama 0.32.0, server live on `:11434`. Both models pulled and probed via
`POST /api/embed`.

| | `nomic-embed-text` | `qwen3-embedding:0.6b` |
|---|---|---|
| Dimensions (measured) | **768** | **1024** |
| L2 norm of output (measured) | **1.0000** | **1.0000** |
| Context | 8192 native; Ollama card caps at 2K unless `num_ctx` is set | 32K |
| Download | 274 MB | 639 MB |
| License | Apache-2.0 | Apache-2.0 |
| MTEB | 62.3 (English v1) | ~70.7 (MTEB-eng-v2) |

Throughput, ~307-token inputs, batch size vs. items/sec:

| batch | nomic | qwen3:0.6b |
|---|---|---|
| 1 | 4.2/s | 2.8/s |
| 8 | 32.3/s | 46.3/s |
| 32 | 34.9/s | 65.1/s |
| 64 | 35.8/s | **79.8/s** |

**Three consequences:**

1. **Both models return pre-normalised vectors.** Cosine similarity is
   therefore a plain dot product. Store no magnitude, normalise nothing.
   Verify the norm on first read per model so the assumption cannot rot
   silently if a future provider misbehaves.
2. **Batching is worth 8–28x.** Ollama honours array `input`. The optimal
   batch differs per model (nomic plateaus ~8, qwen3 keeps scaling to 64), so
   batch size must be configurable rather than a constant.
3. **The bigger model is the faster one here.** qwen3:0.6b is 2.2x nomic's
   throughput *and* scores better. The intuition that the smaller model is the
   cheaper one does not hold on this hardware. This is why the bake-off is
   worth running rather than just picking.

### Per-model input templates are mandatory

Measured/documented, and a **correction to the August notes on #30**, which
said the provider abstraction was just "base URL, model name, optional API
key". It is not: every serious embedding model wants model-specific input
formatting, and the template is part of what produced the vector.

| Model | Template |
|---|---|
| `nomic-embed-text` | `clustering: {text}` (also `search_document:`, `search_query:`, `classification:`) |
| `embeddinggemma` | `title: none \| text: {text}` |
| `qwen3-embedding` | `{text}` (instruction preamble only for queries) |

nomic-embed-text shipping a **dedicated `clustering:` prefix** is a real point
in its favour for this issue specifically: clustering was a training task, not
an afterthought.

## 3. The finding that reshaped the design

`internal/database/db.go:48` sets **`conn.SetMaxOpenConns(1)`**.

One connection. An open `*sql.Tx` has that connection checked out, so nothing
else in the process can touch the database until it commits. WAL does not
help: the contended resource is the *connection*, not the write lock.
`internal/database/pagination.go:462` already documents this hazard by name.

`RunBackfill` opens a transaction and calls **both** `NextBatch` and
`Recompute` inside it. That is correct for `item_text`, where `Recompute` is
pure CPU and takes microseconds. It is wrong for embeddings: a `Recompute`
that makes an HTTP call holds the whole database hostage for the round trip.

At the default batch size of 500 and ~80 items/s, that is **~6 seconds of
total database unavailability per batch**, against a `busy_timeout` of 5000 ms
(`db.go:25`). A `feedspool serve` sharing the process would start failing
requests.

**So #58's doc comment is not quite right.** The *interface* fits embeddings;
the *driver* does not. Hence `RunStagedBackfill` (spec §4).

The tempting non-fix, recorded so it is not re-attempted: "do all the HTTP
first, then all the writes, inside the existing `Recompute`." This does not
work. The transaction is already open by then, so the connection is already
checked out regardless of when the writes happen.

## 4. Date-window research

- `items` has `published_date` (nullable) and `first_seen` (nullable, added by
  migration 4, backfilled).
- **`fetcher.clampItemDate` (`internal/fetcher/fetcher.go:246`) already
  sanitises `published_date` on write**: future dates clamp down to
  `first_seen` (or `now()`), ancient dates clamp up to
  `database.MinReasonableItemDate`. The column is bounded on both ends.
  This substantially de-risks windowing on `published_date`, which was
  otherwise the known-unreliable option — the repo has branch history about
  future-dated items.
- **Flag naming is a trap here.** `cmd/build.go:30` documents that
  `--max-age` *already* means two opposite things in this codebase:
  `fetch --max-age` skips feeds fetched recently, `render --max-age` is a
  display window. A third meaning would make it worse. Avoid the name.
- **`database.ParseTimeWindow(maxAge, start, end)` already exists** and
  handles a duration *or* an explicit range, defaulting to 24h.
  `database.ParseDuration` accepts `2d`, `3h`, `1w`. Reuse wholesale.

### The effective-date expression already exists, and is already indexed

This corrects two things I got wrong earlier in this same session, so both the
wrong and right versions are recorded:

**Wrong:** "`items --since/--until` filter on `first_seen`." That is what
`cmd/items.go:33` *says*, but not what the code does.

**Right:** `buildItemsQuery` (`internal/database/item_repository.go:475`) —
the query behind `feedspool items --since/--until` — filters on
`aliasedEffectiveDateExpression`. The underlying constants
(`item_repository.go:20-28`):

```go
const effectiveDateExpression = `julianday(COALESCE(published_date, first_seen))`
// plus effectiveDateSinceClause / effectiveDateUntilClause, which carry an
// IS NOT NULL guard, and an `i.`-aliased variant for joined queries.
```

So **windowing on `COALESCE(published_date, first_seen)` is not a divergence
from the `items` convention — it is exactly the `items` convention.**
`cmd/items.go:33`'s help text is stale and describes behaviour the code does
not have. That is a pre-existing doc bug, out of scope here, recorded for
later.

**Wrong:** "a `COALESCE(published_date, first_seen)` expression no index can
serve," and the open question of whether we need to add one.

**Right:** **migration 9 already created the expression indexes**
(`migrations.go:90-92`):

```sql
CREATE INDEX idx_items_effective_date      ON items(julianday(COALESCE(published_date, first_seen)));
CREATE INDEX idx_items_feed_effective_date ON items(feed_url, julianday(COALESCE(published_date, first_seen)));
```

Migration 12 therefore needs **no** index for the window, and the open question
is closed.

**Load-bearing consequence:** SQLite only uses an expression index when the
query's expression matches the indexed one *textually*. So the embed predicate
must reuse `effectiveDateExpression` / `aliasedEffectiveDateExpression` rather
than hand-writing an equivalent `COALESCE`. Reusing the constant is what makes
the index apply; an equivalent-but-differently-spelled expression silently
falls back to a full scan.

## 5. Corpus and scale — measured, not projected

Real spool at `data/feeds-backup.db` (551 MB), copied to
`/tmp/feedspool-issue30/spool.db` and measured 2026-09-15. **The original was
never opened** — opening it would run migrations against it. Work on a copy.

**460 feeds, 34,613 items** (larger than the 19,750-item corpus #58 used).
Bringing the copy up to schema 11 (the current head) and backfilling all
34,613 `item_text` rows took **17.6 s** wall.

### Window sizes — the numbers the design turns on

| window | items | p50 tokens | p90 | p99 | max |
|---|---|---|---|---|---|
| last 1d | **919** | 100 | 786 | 2,955 | 11,727 |
| last 2d | **1,898** | 99 | 762 | 3,844 | 14,426 |
| last 3d | **2,548** | 94 | 694 | 3,621 | 14,426 |
| last 7d | 6,275 | 90 | 694 | 4,195 | 14,426 |

(Token estimates are stripped-text chars / 4. A 2-day window is ~2.3 MB of
stripped text, ~587k tokens.)

**Real items are much smaller than expected.** The median is ~100 tokens — the
307-token sample used for the throughput benchmark in §2 was atypically long,
so real-world throughput should beat those numbers rather than match them.

### Context-window overflow in a 2-day window

| context | items over | share |
|---|---|---|
| 256 tok | 379 / 1,898 | 20.0% |
| **2,048 tok** | **44 / 1,898** | **2.3%** |
| 8,192 tok | 5 / 1,898 | 0.3% |
| 32,768 tok | 0 / 1,898 | 0.0% |

**Actionable:** Ollama's model card caps `nomic-embed-text` at 2K context
unless `num_ctx` is set, so the default would silently truncate ~2.3% of a
run. Set `num_ctx: 8192` explicitly (nomic's true native window) and the
overflow drops to 0.3%. qwen3's 32K window covers everything.

### Storage, recomputed on the real corpus

| Model | Per item | 34,613 items |
|---|---|---|
| nomic (768 × f32) | 3,072 B | ~106 MB |
| qwen3 (1024 × f32) | 4,096 B | ~142 MB |
| Both retained | 7,168 B | **~248 MB** (+45% on 551 MB) |

A 2-day window alone is only ~5.8 MB for nomic — the +45% figure is the
worst case of a *full* corpus backfill in both models, not normal operation.

**The vector-index question is emphatically closed.** A dot product over a
2-day window is 1,898 × 768 ≈ 1.5M multiply-adds — microseconds in scalar Go.
The "no vector index under `CGO_ENABLED=0`" constraint from the August notes
never binds at this scale; ANN indexing would not pay for itself until
millions of vectors.

Embedding a 2-day window: 1,898 items at the §2 throughput floor (36 items/s
for nomic on *longer* inputs) is under a minute; a full-corpus backfill of
34,613 items is roughly 7–16 minutes depending on model. Both acceptable.

**Perf note inherited from #58:** `reindex` with nothing to do took 5.07 s
wall / 0.6 s CPU on that corpus — I/O-bound on the staleness scan, not doing
real work. Our `embed` predicate adds a three-way join, but unlike `reindex`
it is **window-scoped and index-served** (§4): `idx_items_effective_date`
already covers the date range, so `embed` starts from a narrow row set instead
of scanning every item. Idle `embed --last 2d` should therefore be
*faster* than idle `reindex`, not slower. Worth confirming in the smoke test
rather than assuming.

## 6. Can we embed in-process, without Ollama?

**Yes — the August note claiming cgo closes this door is out of date.** That
note only considered ONNX Runtime and llama.cpp bindings. Pure-Go inference
frameworks have since caught up.

| Library | Last commit | Latest release | Verdict |
|---|---|---|---|
| `knights-analytics/hugot` | 2026-09-02 | v0.7.8 (2026-09-02) | Live, Apache-2.0, 648★ |
| `gomlx/gomlx` (hugot's Go backend) | 2026-09-13 | v0.28.11 (2026-09-08) | Very live, Apache-2.0, 1636★ |
| `nlpodyssey/cybertron` | **2024-06-08** | v0.2.1 (**2023-11**) | Dormant ~2 years — rejected |
| `nlpodyssey/spago` | 2025-04-01 | v1.1.0 (2023-10) | Stale — rejected |

`purego`-based ONNX Runtime bindings (`amikos-tech/pure-onnx`,
`shota3506/onnxruntime-purego`) also build under `CGO_ENABLED=0`, but still
need the ONNX Runtime **shared library present at runtime** — a runtime
dependency, which trades one external requirement for a worse one.

**Measured cost of the hugot + GoMLX path**, `CGO_ENABLED=0`, stripped:
**11 MB** binary (feedspool today: 24 MB unstripped), **344 packages** added
to the dependency graph.

So binary size — the objection I expected to be decisive — **is not a
blocker**. The marginal add is ~9 MB and it builds cgo-free and static.

### Why it is still deferred

hugot's maintainers scope the Go backend to *"smaller models such as
all-MiniLM-L6-v2"* and say plainly: *"If you have performance requirements,
please move to a C backend."* That model has a **256-token context window**:

| | dims | context | MTEB |
|---|---|---|---|
| all-MiniLM-L6-v2 (pure-Go path) | 384 | **256** | ~56 |
| nomic-embed-text | 768 | 8192 | 62.3 |
| qwen3-embedding:0.6b | 1024 | 32K | 70.7 |

**Correction to an earlier draft of this section.** It claimed the 256-token
window "would truncate the majority of real feed items." The corpus
measurements in §5 say otherwise: the median real item is ~100 tokens, and
only **20%** of a 2-day window exceeds 256 tokens. The claim was extrapolated
from a 307-token benchmark sample that turned out to be atypically long. The
truncation argument is real but much weaker than stated.

What survives the correction:

- 20% truncation is not nothing, and it is **biased toward the longest, most
  substantive items** — plausibly the most topically distinctive ones, which is
  the worst place to lose signal when the goal is clustering.
- **The quality gap is the stronger argument and is untouched:** MTEB ~56 vs
  70.7, and 384 dimensions against 1024.
- The model-acquisition subsystem still has to be built either way.

**The framing is experimental, not technical.** The one genuinely open risk in
#30 is whether clustering quality justifies a `topics` command. Answering that
on the weakest available model risks a result that teaches us nothing — we
could not distinguish "clustering is the wrong idea" from "the embeddings were
too weak to cluster." Bad embeddings can sink good clustering.

Two things make deferring cheap rather than lossy:

1. `Provider.Embed(ctx, texts) ([][]float32, error)` has **no HTTP in the
   signature**. An in-process provider is a second implementation behind the
   same interface, added later, with nothing to unwind.
2. "Don't require Ollama" is already *partly* satisfied — the same HTTP
   provider covers OpenAI-compatible hosted endpoints. What in-process
   uniquely buys is *zero-setup offline*, which is a packaging nicety worth
   paying for once the feature is known to be worth shipping.

Also deferred with it: ONNX model download, cache-directory management,
version pinning, and integrity checking — a subsystem Ollama gives us free.

**Revisit when:** `topics` has proven worth shipping AND zero-setup matters.
hugot is the path; it is a contained addition, not a rewrite.

## 7. Reuse confirmed

`internal/httpclient.Request` already carries `Method`, `Body io.Reader`,
`Headers`, and `Context` (`client.go:76`). POST works today; no extension
needed. We inherit its User-Agent and timeout handling.

`internal/config`: `APIConfig.Token` sets the precedent for secrets —
deliberately **no** command-line flag, with the reason in a comment: *"a token
passed on the command line ends up in ps output."* `embed.api_key` follows it.
