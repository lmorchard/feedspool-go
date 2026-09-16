# Session Notes: Embedding Substrate (issue #30, slice A)

**Issue:** [#30](https://github.com/lmorchard/feedspool-go/issues/30)
**Branch:** `feat/30-embeddings` (worktree at `.claude/worktrees/issue-30-embeddings`)
**Status:** All six phases complete and committed. Not yet pushed; no PR opened.

## What shipped

`feedspool embed --last 2d` computes vector embeddings for items in a time
window, and `feedspool related <link>` ranks items by similarity to one item.
Migration 12 adds `item_embeddings`, keyed `(item_id, model_id)` so two models
coexist for the same item.

Underneath: `internal/embed` is one transport-agnostic provider (local Ollama
and a hosted OpenAI-compatible endpoint are the same code path with a different
base URL), and `database.RunStagedBackfill` is a new backfill driver that keeps
network I/O outside any transaction. `internal/database/vector.go` is the
float32 codec and dot product.

See `MANUAL.md` for the user-facing contract (`embed` and `related` subcommand
sections, the `embed:` config block, and `item_embeddings` under Data Model),
and `spec.md` / `research.md` / `plan.md` here for the design rationale and
per-phase record.

## The bake-off verdict: keep nomic-embed-text

Both models embedded the same 1,236-item window into one database, compared by
eye through `related`.

| | nomic-embed-text | qwen3-embedding:0.6b |
|---|---|---|
| Throughput on real items | **29.2 items/s** (1,257 in 43.0s) | 8.7 items/s (1,236 in 2m21s) |
| Dims | 768 | 1024 |
| Reliable `num_ctx` | 8192 | **2048 only** — see below |
| Dense-topic quality | excellent | excellent |
| Sparse-topic quality | **usable** | noise |

**Dense topic** (AI safety/slowdown, a story many feeds covered): both
excellent, substantial overlap, and qwen3's top hit was arguably the single
best match. No meaningful difference.

**Sparse topic** ("Orchids return to grassland after 28 Years Later filming"):
nomic returned *The Pollinator Industrial Complex* (0.742) and *Lost oyster
beds rediscovered* (0.725) — genuinely ecological — before drifting. qwen3
returned *Cunk on Cinema*, *Linkalongday*, and *Mitch McConnell returns to the
Senate*: noise from rank one.

**Verdict: nomic stays the default.** It is 3.4x faster, reliable at a 4x
larger context, and better where it matters — sparse topics, which is precisely
the case daily topic clustering has to handle. A plausible explanation for the
quality gap is that nomic ships a dedicated `clustering:` task prefix and we
use it, while qwen3 gets bare text; if so the prefix is doing real work.

My pre-implementation benchmark predicted the opposite (qwen3 2.2x faster) and
was wrong, because it used uniform synthetic input. Real items vary hugely in
length and Ollama processes a batch at its longest member, so a large batch of
mixed lengths behaves nothing like a batch of identical ones.

## Measured on the real spool

Copy of `data/feeds-backup.db` (551 MB, 460 feeds, **34,613 items**). **Never
open the original** — any command applies migrations in place.

| | |
|---|---|
| Migration 11→12 | **0.62s** wall, 0.05s CPU (DDL only), 0.034s no-op on re-run |
| Window sizes | 919 items/1 day, ~1,250/2 days, 2,548/3 days |
| `embed --last 2d`, nomic | 43.0s |
| Idle `embed --dry-run` | **0.100s** vs idle `reindex` 1.544s — the window is index-served |
| Storage | ~3 KB/item/model; ~106 MB nomic, ~248 MB both, over the full corpus |

`embed --dry-run` reported 1263 items and `items --since/--until` over the
identical instants returned 1263 — exact agreement, which is what reusing
`aliasedEffectiveDateExpression` was for.

## Three findings worth carrying forward

### 1. Similarity thresholds must be relative, not absolute

For one subject against 1,256 candidates under nomic: **median 0.586**, p75
0.623, p95 0.748, p99 0.815, max 0.869, min 0.404.

Unrelated feed items score ~0.59, not 0 — the familiar anisotropy of embedding
models, where all vectors occupy a narrow cone. So:

- **An absolute threshold will not work.** "similarity > 0.5" matches
  essentially everything.
- **The signal is the top 1–5%.** `>= 0.85` gave 4 items, `>= 0.80` gave 18,
  `>= 0.75` gave 60.
- **Scale is per model.** The same genuine matches sat near 0.65 under qwen3
  and unrelated ones near 0.35.
- **Cluster density shows up in the magnitude**: the dense AI topic reached
  0.87, the sparse orchid one topped out at 0.74.

**For slice B:** clustering wants a distribution-aware or relative threshold
(k-nearest, or a percentile of the observed distribution), not a constant. This
is the most useful thing slice A produced.

### 2. `qwen3-embedding:0.6b` is unreliable above `num_ctx: 2048`

Measured against Ollama 0.32.0. At `num_ctx: 8192` its runner dies on long
inputs — HTTP 400 `do embedding request: ... EOF` — at an *unstable* threshold:
a 12,440-char item failed alone in one run, while a binary search put the limit
near 24,891 chars in another. At `num_ctx: 2048` the same model accepts 200,000
characters happily. nomic is reliable at both.

Two consequences, both now in the code:

- qwen3's default `num_ctx` is **2048**, not its advertised 32768. A reliable
  small window beats a flaky large one.
- The provider **truncates each input** to `num_ctx * 3` characters before
  sending. Relying on a provider's over-length behavior is not safe, and the
  spool's largest item is ~57,000 characters. This makes the truncation ours:
  explicit, tested, and the same for every provider.

### 3. `published_date` is *nearly* always sanitized

`fetcher.clampItemDate` clamps future dates to `first_seen`, and it works:
exactly **1 item in 24,156** has a future `published_date` (dated a month
ahead). `research.md` §4 says the column is bounded on both ends; that is
substantially true but not absolute.

That one item cost real time. The smoke script initially picked a subject with
an open-ended `--since`, which sorted the future-dated item first — so `embed`
had correctly skipped it and `related` correctly reported it unembedded, and it
looked like a bug in both. The script now bounds the subject window at both
ends, matching `embed`'s. Amusingly it found the corpus's single weirdest item
on the first attempt, which is a decent argument for the script existing.

## Things I fixed that were not in the plan

- **`rewindPastMigration11`** (`item_text_test.go`, from #58) deleted only
  `version = 11` from `schema_migrations`, but `GetMigrationVersion` reads
  `MAX(version)`. Once 12 existed the "rewound" database still reported itself
  migrated, `RunMigrations` did nothing, and six of #58's tests failed on a
  missing `item_text` rather than on what they assert. Now `version >= 11`. The
  helper was only accidentally correct while 11 was the head — there is a
  comment saying so, for migration 13.
- **`FEEDSPOOL_EMBED_API_KEY`** was documented in the spec and in a config
  comment but would have silently done nothing: `root.go` calls
  `viper.AutomaticEnv()` with no prefix or key replacer, so a dotted key has no
  usable environment spelling. Added the explicit `viper.BindEnv`, matching
  `serve.api.token`.
- **`resolveItem` extracted from `cmd/item.go`** so `related` shares the
  ambiguity handling rather than duplicating it. That paid off immediately: the
  BBC news feed carries some articles under two GUIDs with one URL, and
  `related` refuses it with the same message `item` gives.
- **`make check`** added (format → lint → test) as its own commit, since the
  three-command pass appears in every phase.

## What is deliberately not here

Clustering, `feedspool topics`, and LLM topic labels — that is slice B, and the
threshold finding above is the main input to it.

**In-process embedding is deferred, not overlooked.** It is feasible cgo-free
via hugot + GoMLX (measured: ~9 MB binary growth, 344 packages, builds under
`CGO_ENABLED=0`). Binary size was the objection I expected and it is not one.
It is deferred because the pure-Go path forces a much weaker model (MTEB ~56
against nomic's 62.3 and qwen3's 70.7), and answering "is clustering good
enough?" on the weakest available model risks a result that teaches nothing.
`Provider` has no HTTP in its signature, so this is a second implementation
later, not a rewrite. Full reasoning in `research.md` §6.

Also not done: retry/backoff on provider failures (a transient Ollama crash
aborts the run — recoverable, since the backfill resumes), pruning old models'
vectors, an HTTP API surface, and dimension truncation.

## What I would check first next session

1. **`git push` and open the PR** — nothing is pushed yet.
2. The smoke script needs a **fresh** spool copy; it detects an
   already-embedded one and says so rather than asserting nothing.
3. If starting slice B, read finding #1 above before designing the clustering
   threshold. It is the constraint most likely to be got wrong from intuition.
4. `scripts/smoke-embed.sh <copy> [model] [window]` reproduces the whole
   end-to-end verification in about a minute for nomic.
