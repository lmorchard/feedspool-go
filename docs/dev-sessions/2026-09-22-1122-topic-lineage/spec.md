# Topic Lineage and Label Inheritance Spec (issue #78, slice 1)

**Goal:** Give each trending topic a stable identity across hourly `feedspool topics`
runs, and reuse the previous label for a topic whose membership has not
materially changed — so labels stop flickering and most LLM calls disappear.

**Source:** [#78](https://github.com/lmorchard/feedspool-go/issues/78), the
measurements in its comment, and the brainstorm on 2026-09-22.

## Current state

Each run is an independent snapshot. `Pipeline.Generate` clusters, filters,
labels every cluster concurrently, and `InsertTopicRun` writes `topic_runs`,
`topics`, `topic_items` (`research.md` §1). Nothing links a topic to any
earlier run, and nothing in the topic tables records a fingerprint (§6).

Measured on 167 hourly production runs (`scripts/topic-lineage`, committed
as the first commit on this branch): **96% of topics have a predecessor with
identical membership** (Jaccard exactly 1.0); any threshold from 0.4 to 0.6
classifies identically; matching against the run 6 hours back still finds
92%; 2 splits and 0 merges in 166 pairs. Among topics whose membership did
not change, the LLM reproduced the same label **26%** of the time.

## Desired end state

```
feedspool topics                  # as today, but survivors inherit their label
feedspool topics --no-inherit     # label every cluster fresh (model switch, testing)
feedspool topics --json           # each topic gains thread_id, set_hash, label_source, transition
```

After a run, the log summarises `N topics: X inherited, Y generated, Z new threads`.

Migration 14 adds `topic_threads` and a 1:1 `topic_lineage` row per topic, and
**backfills lineage over every retained run** so the first post-upgrade run
already inherits. `topics.html` is unchanged in this slice; the only visible
effect is that labels stop changing hour to hour.

## Data model

```sql
CREATE TABLE IF NOT EXISTS topic_threads (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    first_seen_at DATETIME NOT NULL,   -- created_at of the run that opened the thread
    last_seen_at  DATETIME NOT NULL,   -- created_at of the latest run with a topic in it
    label         TEXT     NOT NULL,   -- current label; what survivors inherit
    labeled_at    DATETIME NOT NULL    -- run created_at when label was last generated
);
CREATE TABLE IF NOT EXISTS topic_lineage (
    topic_id     INTEGER PRIMARY KEY REFERENCES topics(id) ON DELETE CASCADE,
    thread_id    INTEGER NOT NULL REFERENCES topic_threads(id),
    set_hash     TEXT    NOT NULL,     -- first 32 hex of sha256 over sorted item IDs
    label_source TEXT    NOT NULL      -- 'generated' | 'inherited'
);
CREATE INDEX IF NOT EXISTS idx_topic_lineage_thread ON topic_lineage(thread_id);
CREATE INDEX IF NOT EXISTS idx_topic_lineage_hash   ON topic_lineage(set_hash);
```

`Topic` gains `ThreadID`, `SetHash`, `LabelSource` (read via a LEFT JOIN so
pre-lineage rows still load). `topic_threads` has **no** FK to runs: threads
outlive the runs that created them. Purge deletes threads that no longer have
any `topic_lineage` row.

## Matching rule (the lineage algorithm)

For each cluster in the new run, in descending cluster size:

1. **Exact:** if any topic in the lookback runs has the same `set_hash`, it is
   the same thread. Inherit thread and label.
2. **Fuzzy:** else compute Jaccard against every topic in the lookback runs;
   take the best. If Jaccard ≥ `lineage_threshold` (0.5) **and** that thread
   has not already been claimed by a larger cluster this run, attach to it.
   Inherit the label iff Jaccard ≥ `inherit_threshold` (0.9); otherwise the
   cluster is labelled fresh and the thread's `label`/`labeled_at` update.
3. **Emerged:** else open a new thread and label fresh.

Lookback = the `lineage_lookback` (6) most recent runs before this one.
Singletons keep today's behaviour (own title, no LLM). `--no-inherit` skips
label reuse but still assigns threads.

## Design decisions

- **Decision:** lineage lives in a 1:1 side table, not new columns on `topics`.
  - **Why:** every migration must be idempotent because `InitSchema` runs
    `schema.sql` *then* all migrations (§3), and `MatchesSchemaFile` diffs
    `sqlite_master.sql` text. `ALTER TABLE ADD COLUMN` is neither idempotent
    nor text-stable against an inline `CREATE`. Pure `CREATE IF NOT EXISTS`
    DDL keeps both properties for free. Semantically identical to the
    "on topics rows" option chosen in brainstorm: one lineage record per
    topic, full history retained, labels age out with purge.
  - **Rejected:** `ALTER TABLE topics` — needs `PRAGMA table_info` guards and
    a hand-tuned `schema.sql` to pass parity. Separate `topic_labels(set_hash)`
    table — loses per-run label history the spike relies on.

- **Decision:** exact set-hash match first, Jaccard second.
  - **Why:** the hash is a pure function of `topic_items`, deterministic, and
    covers 96% of cases without a set comparison. Jaccard only runs for the
    remainder. Hash = first 32 hex of sha256 over ascending `items.id` joined
    by `\n`, mirroring `itemtext.SourceHash`'s truncation (§6).
  - **Rejected:** hashing item hash-IDs (`ids.ItemID`) for purge-survival —
    topics never outlive purge of their items anyway (CASCADE), so nothing gained.

- **Decision:** Jaccard 0.5 to attach, 0.9 to inherit, 6-run lookback; all
  three in `topics:` config, none as flags.
  - **Why:** the data says 0.4–0.6 are indistinguishable, so 0.5 is not a
    tuning knob in practice; 0.9 separates "same item set ± one" from a real
    change; 6 runs bridges a failed hour and is cadence-independent. Config
    keys mirror `max_feed_ratio`/`min_diversity_count` (config-only, §4).
  - **Rejected:** time-based lookback (collapses if cadence drops to daily);
    previous-run-only (one failed hour restarts every thread).

- **Decision:** on a contested thread, the largest cluster wins; the others
  emerge as new threads.
  - **Why:** 2 splits and 0 merges in 166 pairs; the rule only needs to be
    deterministic, not clever. Processing clusters largest-first makes it so.

- **Decision:** inherit regardless of whether `llm_model_id` changed; provide
  `--no-inherit` to force fresh labels.
  - **Why:** a label is a label. A user switching models can force one clean
    relabel rather than the code guessing.

- **Decision:** migration 14 is DDL **plus an in-migration Go backfill**.
  - **Why:** everything needed is already stored; no network. The spike
    processed 167 runs in ~2s. Without it, the first post-upgrade run has no
    threaded predecessors and the page relabels one more time. Follows the
    custom-case pattern in `applySpecificMigration` (§3), as migration 11 did.
    The migration test must assert the DDL/backfill make no network call and
    that a second run is a no-op.
  - **Backfilled thread label = the latest run's label.** Nothing visible
    changes at upgrade; from then on it freezes. (Les leans weakly toward the
    *earliest* label — what forward inheritance would have produced. One-line
    change in the backfill if the upgrade-time churn turns out not to matter.)
  - **Rejected:** separate `feedspool topics backfill` command — an extra step
    every deployment would have to remember, for no safety gain.

- **Decision:** matching logic is a pure leaf package `internal/lineage`.
  - **Why:** both `internal/topics` (live runs) and `internal/database` (the
    migration backfill) need it, and `database` cannot import `topics`. Pure
    functions over `[]int64` sets are trivially unit-testable with no DB.

- **Decision:** thread updates happen inside `InsertTopicRun`'s transaction.
  - **Why:** a run and its lineage should be atomic; a failed insert must not
    leave half-updated threads. `Topic.ThreadID == 0` means "create a thread".

## Patterns to follow

- Migration registration: constants, description map, DDL map, custom case —
  `internal/database/migrations.go:9-25, 33-48, 53-184, 288-316`.
- Migration tests: mirror `migration13_test.go` (`TableExistsOnAFreshDatabase`,
  `IsRegistered` asserting `maxMigrationVersion`, `IsIdempotent`,
  `MatchesSchemaFile` at `:80-112`) and `migration12_test.go:46` `DoesNotEmbed`
  for the no-network assertion. Append DDL to `schema.sql` verbatim.
- Insert transaction shape and `rollbackUnlessDone`: `internal/database/topic.go:31-90`.
- Reader shape and time parsing: `topic.go:94-173`, `time_utils.go:33-71`.
- Config: `TopicsConfig` (`config.go:168-180`), `getIntWithDefault`/
  `getFloat64WithDefault` (`:248-260`), constants (`:57-63`), `GetDefault()`
  (`:318-328`), viper defaults (`cmd/root.go:99-104`).
- Flag/config precedence: `resolveOption` (`cmd/topics.go:107-116`); boolean
  `--no-inherit` follows `--no-topics` in `cmd/build.go:48`.
- Hash truncation: `internal/itemtext/itemtext.go:78-83`.
- Fake `Labeler` for pipeline tests: none exists — add one in
  `internal/topics` that records calls; the load-bearing assertion is that a
  second run over identical clusters makes **zero** calls.
- Purge: extend `DeleteTopicRuns` (`topic.go:178-208`) with orphan-thread
  cleanup in the same statement group; extend `TestDeleteTopicRuns`.
- Smoke check against real data: copy `data/feeds.db` first (**never open the
  original** — every command migrates in place), run the migration, and
  compare thread counts with `make topic-lineage LAST=0` (122 threads at
  adjacent-only matching; 6-run lookback should yield slightly fewer).

## What we're NOT doing

- **No render changes.** `topics.html` keeps `#topic-{{.ID}}` anchors; no
  badges, sparklines, or thread permalinks. Slice 3.
- **No trend metrics** (new-since-last-run, histograms, velocity, distinct-feed
  score). Slice 2. Transitions remain derivable from `topic_lineage` + run order.
- **No run-level discontinuity flag** for mass-emerge events (the Sep 18 config
  change). Derivable later; not needed to inherit labels.
- **No centroid storage or drift detection.** Jaccard alone is sufficient on
  this data; revisit if a shorter window changes the distribution.
- **No clustering changes** — threshold, `min_items`, diversity filter untouched.
- **No changes to `related`, `embed`, the API, or `build` flags** beyond
  `topics` inheriting `--no-inherit`'s default (`build` never passes it).
- **Pre-existing doc drift left alone** (record, don't fix): `feedspool.yaml.example`
  writes `default_last` where code reads `topics.last`; MANUAL says
  `--min-items` defaults to 5 (code: 7); `--max-items`/`--no-topics` undocumented.
  The MANUAL section *is* updated for the new config keys and flag.

## Open questions

- **Should the table output mark inherited vs generated?** Default: no; the
  summary log line and `--json` carry it. Table stays as today.
- **Should `topic_threads.label` update when a survivor is relabelled below
  0.9 but the LLM returns the same text?** Default: yes, `labeled_at` moves;
  harmless and simpler than a string compare.
- **Backfill ordering for topics within one run?** Default: descending
  `score` (cluster size), the same order the live rule uses, so backfill and
  live runs resolve contested threads identically.
