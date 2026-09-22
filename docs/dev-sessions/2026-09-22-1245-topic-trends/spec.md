# Topic Trend Data Spec (issue #78, slice 2)

**Goal:** For each topic in a run, compute explainable trend signals — is it
new, growing, fading, how many feeds carry it, what changed since the last run,
and how its items spread over the window — and expose them on the CLI, so
slice 3 can render them.

**Source:** [#78](https://github.com/lmorchard/feedspool-go/issues/78) §4, slice
1 (#79) as foundation, brainstorm on 2026-09-22.

## Current state

Slice 1 gave every topic a `ThreadID` that is stable across runs, stored in
`topic_lineage`, with `topic_threads.first_seen_at` recording when a thread
opened (`research.md` §2). Nothing reads threads back yet: there is no query
for a thread's previous topic, nor for thread timestamps. Item dates are UTC
and windows are relative to now, not calendar days (§4). No command has
subcommands (§3). `internal/api` does not expose topics (§5).

## Desired end state

```
feedspool topics latest             # most recent run with trends, table; no LLM, no writes
feedspool topics latest --json      # same, JSON
feedspool topics --json             # generation output gains the same trend fields
```

Table output of `topics latest`, one row per topic, score order:

```
STATUS   ITEMS  FEEDS  24H  PREV24H  +NEW  -GONE  DAILY          LABEL
new         12      7   12        0    12      0  ▁▁▁▁▁▁█        Xbox layoffs
growing     22     14    6        2     1      0  ▂▃▃▄▅▆█        EU Canada associate membership
fading       5      4    0        3     0      2  ▇█▅▃▁▁▁        AI slowdown debate
steady       8      6    1        1     0      0  ▃▃▄▃▃▃▃        House votes Black contempt
```

JSON adds, per topic, a `trend` object:

```json
"trend": {
  "status": "growing",
  "distinct_feeds": 14,
  "last_24h": 6,
  "prior_24h": 2,
  "new_items": 1,
  "dropped_items": 0,
  "daily": [2, 3, 3, 4, 5, 6, 6],
  "thread_first_seen": "2026-09-18T17:01:45Z"
}
```

## Definitions (all anchored on the run, never on wall-clock now)

Let `end = run.WindowEnd`, and bucket items by `EffectiveDate()`.

- **`daily`**: counts of the topic's items in consecutive 24-hour buckets
  ending at `end`, oldest first, covering `run.WindowStart..end`.
  `len = ceil((end - start) / 24h)`. Items outside the window (possible via
  `first_seen` fallback) are clamped into the first/last bucket.
- **`last_24h`** = last bucket; **`prior_24h`** = the one before (0 if the
  window is ≤ 24h).
- **`distinct_feeds`**: distinct `FeedURL` among the topic's items.
- **`new_items` / `dropped_items`**: set difference against the same thread's
  topic in the **most recent earlier run that contains that thread**. A new
  thread has `new_items = len(items)`, `dropped_items = 0`.
- **`thread_first_seen`**: `topic_threads.first_seen_at`.
- **`status`**, first match wins:
  1. `new` — `thread_first_seen >= end - 24h`
  2. `growing` — `last_24h > prior_24h`
  3. `fading` — `last_24h < prior_24h`
  4. `quiet` — `last_24h == 0` (and so `prior_24h == 0`)
  5. `steady` — otherwise

  *Revised during execution:* `quiet` added after the real-data smoke showed
  36 of 37 "steady" topics had nothing in either window.

## Design decisions

- **Decision:** compute on read; no migration.
  - **Why:** every input is already stored (items, lineage, threads). A pure
    function over plain data is trivially testable and slice 3's renderer can
    call the same code. Chosen in brainstorm.
  - **Rejected:** a persisted `topic_trends` table — duplicates derivable data,
    needs migration 15 + backfill, and goes stale if the rule changes.

- **Decision:** a pure leaf package `internal/trends` with
  `Compute(in Input) Trend`, fed by a loader in `internal/topics`.
  - **Why:** same shape as `internal/lineage`: all the arithmetic is testable
    without a database, and both CLI paths and the future renderer share it.

- **Decision:** buckets are 24h slices ending at `run.WindowEnd`, not calendar
  days.
  - **Why:** windows are relative-to-now in UTC (`research.md` §4); calendar
    days would need a timezone choice and give a partial first/last day.
    Anchoring on the run makes the result reproducible for an old run.

- **Decision:** explainable counts + histogram, and a five-state status with a
  fixed precedence; no gravity score.
  - **Why:** anyone can check why a topic says "growing" from two numbers
    printed next to it. Chosen in brainstorm. A ranking score can come later
    on top of these fields.
  - **Rejected for now:** thresholds/hysteresis on growing vs fading (e.g.
    "≥ 2 more"). Start literal; tune in slice 3 once it's visible.

- **Decision:** previous topic = the thread's topic in the most recent earlier
  run that has one, regardless of the lookback setting.
  - **Why:** lookback governs matching, not reporting; a thread that skipped
    an hour should diff against where it last was, not report everything new.

- **Decision:** `topics latest` is a cobra subcommand of `topics`.
  - **Why:** it's the same noun, read-only, and keeps `topics` (generate)
    behaviour unchanged. First subcommand in the repo; `topicsCmd` keeps its
    `RunE`, so plain `feedspool topics` still generates.
  - **Rejected:** a separate top-level `trends` command — splits one noun.

- **Decision:** generation output (`topics --json`) gains the same `trend`
  object, computed after the run is inserted by the same loader.
  - **Why:** one JSON shape for both surfaces; cron logs show trends too.

## New database reads (`internal/database/topic.go`)

- `GetPreviousThreadItems(ctx, before time.Time, threadIDs []int64) (map[int64][]int64, error)`
  — for each thread, item IDs of its topic in the most recent run with
  `created_at < before`. One query: rank lineage rows per thread by run time,
  take rank 1.
- `GetThreadFirstSeen(ctx, threadIDs []int64) (map[int64]time.Time, error)`.

## Patterns to follow

- Pure package + tests: mirror `internal/lineage` (`lineage.go`, table tests).
- Reads: `GetTopicsForRun` / `GetTopicItems` shape (`topic.go:135-190`), IN-list
  placeholders as in `topicItemSets` (`topic_lineage_backfill.go:185`) with the
  same `//nolint:gosec` form, times through `parseDatabaseTime`.
- Test fixtures: `seedItem` (`item_embedding_test.go:33`), `InsertTopicRun`.
- CLI: `tabwriter` table as in `cmd/show.go:170-185`; JSON via indented
  encoder; `cfg.JSON` switch as in `cmd/status.go:58`. `cmd/topics.go` is
  already on the forbidigo allowlist.
- Pipeline tests reuse `newTopicsTestDB`/`embed`/`fakeLabeler` in
  `internal/topics/pipeline_test.go`.

## What we're NOT doing

- **No render changes** (`topics.html`, badges, sparklines in HTML). Slice 3.
- **No gravity/velocity score, no ranking toggle.**
- **No persistence, no migration.**
- **No `internal/api` exposure** of topics or trends.
- **No thread history view** (`topics thread <id>`) — only latest run.
- **No per-site filtering** of trends; slice 3 decides how filtered item sets
  interact with trends.
- **No changes to clustering, lineage matching, or labeling.**

## Open questions

- **Unicode sparkline in the table?** Default: yes (`▁▂▃▄▅▆▇█`), scaled to
  the topic's own max; `--json` carries raw counts.
- **Items whose effective date falls outside the window?** Default: clamp into
  the edge bucket (stated above) rather than drop, so `sum(daily) == ITEMS`.
- **Status for a run whose window is ≤ 24h?** Default: `prior_24h = 0`, so any
  non-new topic with items in the last bucket reads `growing`. Documented in
  MANUAL; acceptable because the default window is 7d.
