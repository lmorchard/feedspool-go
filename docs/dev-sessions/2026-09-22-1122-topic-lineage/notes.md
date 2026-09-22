# Session Notes: Topic Lineage and Label Inheritance (issue #78, slice 1)

**Issue:** [#78](https://github.com/lmorchard/feedspool-go/issues/78)
**Branch:** `feat/78-topic-lineage` (worktree `.claude/worktrees/78-topic-lineage`)
**Status:** All five phases executed and committed. Not pushed; no PR yet.

## What shipped

- `internal/lineage`: pure set hash + Jaccard + `Assign`. Exact hash first,
  best Jaccard ≥ 0.5 second, else new thread; label inherited at ≥ 0.9.
  Largest cluster wins a contested thread; results are order-independent.
- Migration 14: `topic_threads` + 1:1 `topic_lineage` (pure `CREATE IF NOT
  EXISTS`), with an in-migration Go backfill over every retained run.
- `InsertTopicRun` writes lineage in its transaction via `writeTopicLineage`,
  shared with the backfill. `GetLineageCandidates(before, lookback)`.
  `DeleteTopicRuns` drops orphaned threads.
- Pipeline: lineage step before labeling; inherited topics skip the LLM;
  summary log `Labeled N topics: X inherited, Y generated, Z new threads`.
- Config `topics.lineage_lookback` / `lineage_threshold` / `inherit_threshold`,
  flag `topics --no-inherit`, `--json` gains `thread_id`, `set_hash`,
  `label_source`, `transition`. MANUAL and example config updated.

## Measured on a copy of the production spool

167 hourly runs, 8,079 topics, 7-day window. Migration 14: **1.3 s**. Every
topic threaded (8,079 lineage rows). **121 threads** vs the spike's 122 at
adjacent-only matching — the six-run lookback bridged exactly one gap. Five
threads span all 167 runs; 25 span 102 (the Sep 18 17:00 config-change
discontinuity the spike found). `make topic-lineage` still runs on the
migrated copy and reports the same numbers as before.

Live LLM smoke on the copy with the production config (gemini-2.5-flash via
LiteLLM, clustering threshold 0.82, min 4 items): first post-migration run
**"Labeled 69 topics: 67 inherited, 2 generated, 0 new threads"** -- the two
generated ones attached to a thread at Jaccard between 0.5 and 0.9 and were
relabeled, as designed. Second run: **"69 inherited, 0 generated"**. Two LLM
calls where the old code would have made 138.

## Decisions worth remembering

- **Side table, not `ALTER TABLE`.** A fresh DB runs `schema.sql` then every
  migration, and the parity test diffs `sqlite_master` text; `ALTER` breaks
  both. Same semantics as columns on `topics`.
- **`before` timestamp on the candidates query** is what lets one function
  serve live runs (pass `run.CreatedAt`, not yet inserted) and the backfill
  (pass each historical run's `CreatedAt`).
- **Backfill reads through its transaction** (`queryer` interface). With
  `SetMaxOpenConns(1)` a `db.conn` query inside a tx blocks until busy_timeout.
- **Backfilled thread label = latest run's label** (Inherit off during
  replay, so each survivor relabels). Les leans weakly toward *earliest*;
  that is `Inherit: true` in `backfillRunLineage` and nothing else.
- Migration 13's test no longer pins `maxMigrationVersion`; 14's does — the
  same hand-off 12 → 13 made.

## Copilot review round (PR #79)

Six comments, all acted on:

- **Concurrent backfill could hit `SQLITE_BUSY_SNAPSHOT`** (deferred read, then
  write, after another process committed). Each run's transaction is now wrapped
  in `retrySQLiteBusy`; `isSQLiteBusy` already matches the BUSY subcode via
  `& 0xff`, and the `NOT EXISTS` predicate makes the re-run find those topics
  already threaded.
- **Backfill ignored configured thresholds/lookback.** `DB.SetLineageOptions`
  is installed by `cmd.openDatabase` before `IsInitialized`; the backfill uses
  it with Inherit forced off. `TestMigration14BackfillHonorsConfiguredRule`.
- **Spike script was not read-only and needed migration 14.** It now opens
  with `mode=ro` through `database/sql` directly and reads only the
  migration-13 tables. Verified on an unmigrated copy: mtime/size unchanged,
  still schema 13, same numbers.
- **Misleading comment** about `Inherit: true` reworded; MANUAL's "Current
  version: 12" (already stale) is now 14; the backfill logs "Threaded N/M topic
  runs" instead of borrowing the item-text logger.

## Not done / deliberately out of scope

- No render changes (`topics.html` still anchors on `#topic-{{.ID}}`), no
  trend metrics, no discontinuity flag, no centroids. Slices 2 and 3 of #78.
- Pre-existing doc drift left alone: `feedspool.yaml.example` writes
  `default_last` where code reads `topics.last`; MANUAL says `--min-items`
  defaults to 5 (code: 7); `--max-items` / `--no-topics` undocumented.

## What I would check first next session

1. `make topic-lineage` after a day of live runs: label agreement among
   survivors should be ~100% now instead of 26%.
2. If the upgrade-time page churn is not a concern after all, flip the
   backfill to earliest-label (one-line change, see Decisions).
