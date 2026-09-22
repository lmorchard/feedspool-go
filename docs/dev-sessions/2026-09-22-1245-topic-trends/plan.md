# Topic Trend Data — Implementation Plan

**Goal:** Compute per-topic trend signals (status, distinct feeds, last/prior
24h, new/dropped items, daily histogram, thread first-seen) on read, and show
them in `topics --json` and a new read-only `topics latest` subcommand.

**Approach:** A pure leaf package `internal/trends` owns all arithmetic. Two new
database reads supply thread history. A loader in `internal/topics` gathers
inputs for a run and calls `trends.Compute` per topic; both CLI paths (and
slice 3's renderer) use that loader. No migration.

**Tech stack:** Go 1.26, `modernc.org/sqlite` v1.57 (SQLite 3.53 — window
functions available, probed), cobra, testify. `make check` = format → lint → test.

Shared vocabulary (defined in Phase 1, used everywhere):
- `trends.Status*` = `"new" | "growing" | "fading" | "steady"`
- `trends.Item{FeedURL string; At time.Time}`
- `trends.Input{WindowStart, WindowEnd time.Time; Items []trends.Item; ItemIDs, PrevItemIDs []int64; HasPrev bool; ThreadFirstSeen time.Time}`
- `trends.Trend{Status string; DistinctFeeds, Last24h, Prior24h, NewItems, DroppedItems int; Daily []int; ThreadFirstSeen time.Time}` with JSON tags `status, distinct_feeds, last_24h, prior_24h, new_items, dropped_items, daily, thread_first_seen`
- `trends.Sparkline(daily []int) string`
- **Revised in Phase 1:** `trends.Compute(in *Input) Trend` takes a pointer (gocritic hugeParam: Input is 152 bytes). Callers pass `&trends.Input{...}`.
- `topics.LoadTrends(ctx, db, run, topics, topicItems) (map[int64]trends.Trend, error)` — keyed by topic ID

One commit per phase: `Phase N: <name>`. TDD: failing test first.

---

## Phase 1: `internal/trends` — the pure arithmetic

Delivers `Compute` and `Sparkline` with exhaustive table tests. No database.

**Files:**
- Create: `internal/trends/trends.go`
- Test: `internal/trends/trends_test.go`

**Key changes:**

```go
// Package trends turns a topic's items and its thread's history into
// explainable trend signals. Pure: no database, no clock -- everything is
// anchored on the run's window, so an old run always reports the same thing.
package trends

const (
	StatusNew     = "new"
	StatusGrowing = "growing"
	StatusFading  = "fading"
	StatusSteady  = "steady"
	day           = 24 * time.Hour
)

type Item struct {
	FeedURL string
	At      time.Time // EffectiveDate
}

type Input struct {
	WindowStart, WindowEnd time.Time
	Items                  []Item
	ItemIDs                []int64 // this topic's members
	PrevItemIDs            []int64 // the thread's previous topic's members
	HasPrev                bool    // false for a thread with no earlier topic
	ThreadFirstSeen        time.Time
}

type Trend struct {
	Status          string    `json:"status"`
	DistinctFeeds   int       `json:"distinct_feeds"`
	Last24h         int       `json:"last_24h"`
	Prior24h        int       `json:"prior_24h"`
	NewItems        int       `json:"new_items"`
	DroppedItems    int       `json:"dropped_items"`
	Daily           []int     `json:"daily"`
	ThreadFirstSeen time.Time `json:"thread_first_seen"`
}

func Compute(in Input) Trend {
	t := Trend{Daily: dailyBuckets(in), ThreadFirstSeen: in.ThreadFirstSeen}
	n := len(t.Daily)
	t.Last24h = t.Daily[n-1]
	if n > 1 {
		t.Prior24h = t.Daily[n-2]
	}
	t.DistinctFeeds = distinctFeeds(in.Items)
	t.NewItems, t.DroppedItems = diff(in.ItemIDs, in.PrevItemIDs, in.HasPrev)
	t.Status = status(in, t)
	return t
}

// dailyBuckets: ceil(window/24h) buckets ending at WindowEnd, oldest first,
// minimum 1. Bucket i covers (end-(n-i)*day, end-(n-i-1)*day]. Items outside
// the window clamp into the first or last bucket so sum(Daily) == len(Items).
func dailyBuckets(in Input) []int {
	n := int((in.WindowEnd.Sub(in.WindowStart) + day - 1) / day)
	if n < 1 {
		n = 1
	}
	out := make([]int, n)
	for _, it := range in.Items {
		// floor(age/24h) buckets back from the last; with right-closed buckets an
		// item exactly k*24h old lands in the bucket that ends there. Negative
		// ages (after end) floor toward zero and clamp below.
		back := int(in.WindowEnd.Sub(it.At) / day)
		idx := n - 1 - back
		idx = max(0, min(n-1, idx))
		out[idx]++
	}
	return out
}

// diff: without a previous topic everything is new and nothing dropped.
func diff(cur, prev []int64, hasPrev bool) (added, dropped int)

func status(in Input, t Trend) string {
	switch {
	case !in.ThreadFirstSeen.Before(in.WindowEnd.Add(-day)):
		return StatusNew
	case t.Last24h > t.Prior24h:
		return StatusGrowing
	case t.Last24h < t.Prior24h:
		return StatusFading
	default:
		return StatusSteady
	}
}

// Sparkline renders counts as ▁▂▃▄▅▆▇█ scaled to the slice's own max; an
// all-zero slice renders as all ▁. One rune per bucket.
func Sparkline(daily []int) string
```

Bucket-edge rule, stated once: an item at exactly `end - k*24h` falls in the
bucket that ends there (half-open on the left, closed on the right). Plain
floor division implements it; a test pins it.

Tests (`trends_test.go`, package `trends`), `end := 2026-09-22T12:00Z`:
- `TestDailyBucketsSevenDayWindow` — window 7d, items at end-1h, end-25h,
  end-25h, end-6d-23h → `Daily == [1,0,0,0,0,2,1]`, `len == 7`.
- `TestDailyBucketsEdgeBelongsToEarlierBucket` — item at exactly end-24h
  lands at index n-2 (the bucket ending at end-24h). Assert `Daily[n-2] == 1`, `Daily[n-1] == 0`. Item at exactly `end` →
  `Daily[n-1] == 1`.
- `TestDailyBucketsClampsOutOfWindow` — item 10d before a 7d window → index 0;
  item 2h after end → last index; `sum == len(items)`.
- `TestDailyBucketsPartialDay` — 36h window → `len == 2`; 1h window → `len == 1`.
- `TestComputeLastAndPrior` — from `[..,2,6]` → `Last24h 6, Prior24h 2`;
  1-bucket window → `Prior24h 0`.
- `TestComputeDistinctFeeds` — 5 items over 3 feed URLs → 3.
- `TestComputeDiff` — cur `{1,2,3,4}`, prev `{2,3,5}` → new 2, dropped 1;
  `HasPrev false` → new 4, dropped 0.
- `TestComputeStatusPrecedence` — table: first-seen end-2h with last<prior →
  `new` (new beats fading); first-seen end-3d with last 6/prior 2 → `growing`;
  2/6 → `fading`; 3/3 → `steady`; first-seen exactly end-24h → `new`
  (boundary inclusive).
- `TestSparkline` — `[0,1,2,4,8]` → `"▁▁▂▄█"`; `[0,0,0]` → `"▁▁▁"`; `[5]` →
  `"█"`; `nil` → `""`. Assert rune count equals `len(daily)`.

**Verification — automated:**
- [x] `go test ./internal/trends/ -v` passes with every test above — **10 tests + 6 status subtests PASS; plus TestComputeCarriesFirstSeen**
- [x] `make check` passes — **0 issues after Compute → *Input and sparkLevels → const**

**Verification — manual:**
- [x] Spot-read `status` against spec "Definitions": precedence new → growing → fading → steady. — **switch order matches; boundary inclusive via !Before(end-24h), pinned by two subtests**

---

## Phase 2: Thread history reads

Delivers the two queries the loader needs.

**Files:**
- Modify: `internal/database/topic.go`
- Test: `internal/database/topic_trends_test.go` (new)

**Key changes:**

```go
// GetPreviousThreadItems returns, for each thread, the item IDs of its topic
// in the most recent run created strictly before `before`. Threads with no
// earlier topic are absent from the map. Deliberately ignores the lineage
// lookback: reporting diffs against where a thread last was, however long ago.
func (db *DB) GetPreviousThreadItems(ctx context.Context, before time.Time, threadIDs []int64) (map[int64][]int64, error)
```
SQL (IN-list built as in `topicItemSets`, with the same
`//nolint:gosec // Safe: only formatting placeholder count, not user input` form):
```sql
WITH ranked AS (
	SELECT l.thread_id, l.topic_id,
	       ROW_NUMBER() OVER (PARTITION BY l.thread_id ORDER BY r.created_at DESC, t.id DESC) AS rn
	FROM topic_lineage l
	JOIN topics t     ON t.id = l.topic_id
	JOIN topic_runs r ON r.id = t.run_id
	WHERE r.created_at < ? AND l.thread_id IN (...)
)
SELECT ranked.thread_id, ti.item_id
FROM ranked JOIN topic_items ti ON ti.topic_id = ranked.topic_id
WHERE ranked.rn = 1
ORDER BY ranked.thread_id, ti.item_id
```
Empty `threadIDs` → `map{}` without querying.

```go
// GetThreadFirstSeen returns topic_threads.first_seen_at for each thread.
func (db *DB) GetThreadFirstSeen(ctx context.Context, threadIDs []int64) (map[int64]time.Time, error)
```
`SELECT id, first_seen_at FROM topic_threads WHERE id IN (...)`, parsed with
`parseDatabaseTime`; a parse error is returned, not ignored.

Tests (use `seedItem`, `newTopicRun`, `InsertTopicRun` from existing test files):
- `TestGetPreviousThreadItems` — three runs t0, t1, t2; thread A present in
  all (items {1,2} → {1,2} → {1,2,3}); thread B only in t0 ({4}) and t2 ({4,5}).
  `before = t2.CreatedAt`, threads [A, B] → A: `{1,2}` (from t1), B: `{4}`
  (from t0, skipping the gap). `before = t0.CreatedAt` → empty map.
- `TestGetPreviousThreadItemsEmptyInput` — `nil` threads → empty, no error.
- `TestGetThreadFirstSeen` — thread opened at t0 then attached at t1 →
  `first_seen == t0` (compare `formatDatabaseTime`); unknown ID absent.

**Verification — automated:**
- [x] `go test ./internal/database/ -run 'PreviousThreadItems|ThreadFirstSeen' -v` passes — **3 PASS; the thread that skipped a run diffs against run 0**
- [x] `make check` passes — **0 issues**

**Verification — manual:**
- [ ] Query returns the expected rows on the migrated production copy (checked in Phase 4 smoke).

---

## Phase 3: `LoadTrends` and trends in generation output

Delivers the shared loader and adds `trend` to `topics --json`.

**Files:**
- Create: `internal/topics/trends.go`
- Test: `internal/topics/trends_test.go`
- Modify: `cmd/topics.go` — `printTopicsJSON` gains `trend`

**Key changes:**

```go
// LoadTrends computes a trends.Trend for every topic in a run, keyed by topic
// ID. topicItems is keyed by topic ID (GetTopicItems' shape). Topics without a
// thread (pre-migration rows) get a trend with HasPrev false and a zero
// ThreadFirstSeen, which Compute reports as not-new.
func LoadTrends(ctx context.Context, db *database.DB, run *database.TopicRun,
	topics []*database.Topic, topicItems map[int64][]int64) (map[int64]trends.Trend, error) {
	threadIDs := unique ThreadIDs (non-zero)
	prev, err := db.GetPreviousThreadItems(ctx, run.CreatedAt, threadIDs)
	firstSeen, err := db.GetThreadFirstSeen(ctx, threadIDs)
	all item IDs → db.GetItemsByIDs
	for each topic:
		ids := topicItems[t.ID]
		items := for id in ids with item present: trends.Item{FeedURL, EffectiveDate()}
		p, hasPrev := prev[t.ThreadID]
		out[t.ID] = trends.Compute(trends.Input{run.WindowStart, run.WindowEnd, items,
			ids, p, hasPrev, firstSeen[t.ThreadID]})
}
```
Purged items (absent from `GetItemsByIDs`) are skipped for dates/feeds but
still count in `ItemIDs` for the diff, so a purge does not look like churn.

`cmd/topics.go`: after `Generate`, build `map[int64][]int64` from `itemsMap`
(keyed by `*Topic`) and call `topics.LoadTrends`; on error, log a warning and
omit `trend` rather than failing a run that already succeeded. `printTopicsJSON`
gains a `trends map[int64]trends.Trend` parameter and emits `"trend"` when
present. Table output unchanged here (Phase 4 adds the table).

Tests (`internal/topics/trends_test.go`, reusing `newTopicsTestDB`, `embed`,
`fakeLabeler`, `generate` from `pipeline_test.go`):
- `TestLoadTrendsAfterTwoRuns` — items 1–3 cluster; run 1; add items 4–6 in
  a second cluster and run 2. For run 2 (from `GetLatestTopicRun`,
  `GetTopicsForRun`, `GetTopicItems`): the surviving topic has a previous
  topic, so `NewItems 0, DroppedItems 0`; its thread opened < 24h ago, so
  `Status == "new"`; and `DistinctFeeds == 1`,
  `sum(Daily) == 3`, `ThreadFirstSeen == run1.CreatedAt` (formatted). The new
  topic: `NewItems 3, DroppedItems 0, Status "new"`.
- `TestLoadTrendsDiffAgainstPreviousTopic` — run 1 with items 1–3; then insert
  a run directly via `db.InsertTopicRun` at run1+1h whose topic carries the
  same `ThreadID` with items {2,3} plus a new item 7 (seed via
  `db.UpsertItem`); `LoadTrends` on that run → `NewItems 1, DroppedItems 1`.
- `TestLoadTrendsToleratesPurgedItem` — call `LoadTrends` with a
  `topicItems` map whose topic also lists a non-existent item ID 999999 (a
  real purge would cascade the `topic_items` row away, so this is the only
  way to reach the path); assert no error, `sum(Daily) == len(real items)`, and `NewItems` counts 999999.

**Verification — automated:**
- [x] `go test ./internal/topics/ -run LoadTrends -v` passes — **3 PASS**
- [x] `make check` passes — **0 issues**

**Verification — manual:**
- [ ] `topics --json` on a test DB shows a `trend` object per topic. — *needs a live LLM run; the same LoadTrends path is exercised on real data by `topics latest` in Phase 4*

---

## Phase 4: `topics latest`, docs, real-data smoke

Delivers the read-only subcommand with table and JSON output, documentation,
and a check against a migrated copy of the production spool.

**Files:**
- Modify: `cmd/topics.go` — `topicsLatestCmd`, table printer
- Modify: `MANUAL.md` — `topics latest` section, `trend` JSON fields, definitions

**Key changes:**

```go
var topicsLatestCmd = &cobra.Command{
	Use:   "latest",
	Short: "Show the most recent topic run with trend signals (no generation)",
	Args:  cobra.NoArgs,
	RunE:  runTopicsLatest,
}

func runTopicsLatest(_ *cobra.Command, _ []string) error {
	cfg := GetConfig()
	db, err := openDatabase(cfg.Database)   // migrates, as every command does
	defer db.Close()
	run, err := db.GetLatestTopicRun(ctx)
	if run == nil → JSON "[]" / "No topic runs yet. Run `feedspool topics` first."
	topicList := db.GetTopicsForRun; items := db.GetTopicItems
	tr, err := topics.LoadTrends(ctx, db, run, topicList, items)   // error is fatal here: it's the whole point
	if cfg.JSON { printLatestJSON(run, topicList, items, tr) } else { printLatestTable(run, topicList, items, tr) }
}
// init(): topicsCmd.AddCommand(topicsLatestCmd)
```

`printLatestJSON` emits `{"run": {id, created_at, window_start, window_end,
embed_model, llm_model}, "topics": [ {label, score, count, thread_id,
set_hash, label_source, trend} ]}` via an indented encoder, using small
structs with snake_case tags (as `cmd/status.go` does) rather than
`map[string]any`.

`printLatestTable` prints a header line with the run time and window, then a
`tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)` table with columns
`STATUS ITEMS FEEDS 24H PREV24H +NEW -GONE DAILY LABEL` in score order,
`DAILY` = `trends.Sparkline(trend.Daily)`.

MANUAL: add `#### topics latest` under `### topics` with usage, the table
example from the spec, the `trend` JSON fields, and the definitions block
(24h buckets anchored on window end, status precedence, previous-topic rule,
≤24h window note). Add `trend` to the `--json` bullet of `topics`.

`cmd/` is not unit-tested by repo convention (CLAUDE.md); coverage lives in
Phases 1–3. TDD opt-out for this phase's CLI wiring, stated here.

Real-data smoke (**copy first; every command migrates in place**):
```sh
mkdir -p /tmp/feedspool-78t && cp ../../../data/feeds.db /tmp/feedspool-78t/spool.db
make build
./feedspool --database /tmp/feedspool-78t/spool.db topics latest
./feedspool --database /tmp/feedspool-78t/spool.db topics latest --json | head -40
```

**Verification — automated:**
- [x] `make build && ./feedspool topics --help` lists `latest` under Available Commands — **listed**
- [x] `make check` passes — **0 issues (command lives in cmd/topics_latest.go, added to the forbidigo allowlist)**

**Verification — manual:**
- [x] On the copy: `topics latest` prints one row per topic of the latest run with no LLM call and no new `topic_runs` row (count before/after). — **71 rows; topic_runs 167 before and after**
- [!] Statuses look plausible against the numbers beside them; the Sep 18 discontinuity threads are not reported `new` (first-seen > 24h before window end). — **Sep 18 threads correctly not `new`. BUT 36 of 37 `steady` topics have 24H = PREV24H = 0: dormant, not steady. The spec's literal rule behaves as written; the rule is missing a state. Raised with Les; see notes.md.** **Resolved: Les chose a fifth status `quiet` (24H = PREV24H = 0). Re-run: 16 new, 4 growing, 14 fading, 36 quiet, 1 steady.**
- [x] `sum(daily) == count` for every topic in the JSON (jq check). — **71 topics, 0 mismatches**
- [x] MANUAL `topics latest` section reads coherently. — **read back; example table uses real rows from the copy**
