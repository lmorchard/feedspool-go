# Topic Lineage and Label Inheritance — Implementation Plan

**Goal:** Give each topic a stable thread across `feedspool topics` runs and reuse
the previous label when membership has not materially changed, with a
migration that backfills threads over every retained run.

**Approach:** A pure leaf package `internal/lineage` owns hashing, Jaccard and
thread assignment so both the live pipeline and the migration backfill use one
rule. Migration 14 adds `topic_threads` and a 1:1 `topic_lineage` table (pure
`CREATE IF NOT EXISTS`, so idempotent and schema-parity-safe) and replays
lineage over existing runs in-process, no network. `InsertTopicRun` writes
lineage inside its existing transaction. Config and a `--no-inherit` flag sit
on top; `topics.html` is untouched.

**Tech stack:** Go 1.26, `modernc.org/sqlite` (cgo-free, `SetMaxOpenConns(1)`),
cobra/viper, testify. `make check` = format → lint → test.

Shared vocabulary used in every phase (defined in Phase 1):
- `lineage.SetHash(items []int64) string` — first 32 hex of sha256 over ascending IDs
- `lineage.Candidate{TopicID, ThreadID int64; ThreadLabel, Hash string; Items []int64}`
- `lineage.Options{AttachThreshold, InheritThreshold float64; Inherit bool}`
- `lineage.Assignment{Hash string; ThreadID int64; Jaccard float64; Label, Transition string}`
- `lineage.SourceGenerated = "generated"`, `lineage.SourceInherited = "inherited"`
- `lineage.TransitionNew = "new"`, `lineage.TransitionSurvived = "survived"`
- `lineage.DefaultLookback = 6`, `DefaultAttachThreshold = 0.5`, `DefaultInheritThreshold = 0.9`

One commit per phase: `Phase N: <name>`. TDD throughout: failing test first.

---

## Phase 1: `internal/lineage` — the pure matching rule

Delivers the set hash, Jaccard, and thread assignment as pure functions with
exhaustive tests. No database. Everything later builds on these types.

**Files:**
- Create: `internal/lineage/lineage.go`
- Test: `internal/lineage/lineage_test.go`

**Key changes:**

```go
// Package lineage matches topic clusters across runs by item-set overlap.
// It is a leaf package: internal/database (the migration backfill) and
// internal/topics (live runs) both import it, so it imports neither.
package lineage

const (
	DefaultLookback         = 6   // runs to look back through for a predecessor
	DefaultAttachThreshold  = 0.5 // Jaccard at or above which a cluster joins a thread
	DefaultInheritThreshold = 0.9 // Jaccard at or above which it also keeps the label
	hashLength              = 32  // hex chars, mirroring itemtext.SourceHash
)

const (
	SourceGenerated    = "generated"
	SourceInherited    = "inherited"
	TransitionNew      = "new"
	TransitionSurvived = "survived"
)

type Candidate struct {
	TopicID     int64
	ThreadID    int64
	ThreadLabel string  // the thread's current label, what a survivor inherits
	Hash        string  // SetHash of Items
	Items       []int64 // ascending
}

type Options struct {
	AttachThreshold  float64
	InheritThreshold float64
	Inherit          bool // false = assign threads but never reuse a label
}

type Assignment struct {
	Hash       string
	ThreadID   int64   // 0 means open a new thread
	Jaccard    float64 // 1 for an exact hash match, 0 for a new thread
	Label      string  // inherited label; "" means label this cluster fresh
	Transition string  // TransitionNew | TransitionSurvived
}

// SetHash is order-independent: it sorts a copy, joins decimal IDs with "\n",
// sha256s, and keeps the first 32 hex characters.
func SetHash(items []int64) string

// Jaccard over two ascending slices via a merge walk. Two empty sets → 0.
func Jaccard(a, b []int64) float64

// Assign resolves every cluster to a thread. Clusters are processed largest
// first (ties: smallest first item ID) so a contested thread goes to the
// largest claimant deterministically. Output is indexed like clusters.
func Assign(clusters [][]int64, candidates []Candidate, opts Options) []Assignment
```

Assign, in full:

```go
func Assign(clusters [][]int64, candidates []Candidate, opts Options) []Assignment {
	sorted := make([][]int64, len(clusters))
	for i, c := range clusters {
		sorted[i] = slices.Sorted(slices.Values(c))
	}
	order := make([]int, len(clusters))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(x, y int) bool {
		a, b := sorted[order[x]], sorted[order[y]]
		if len(a) != len(b) {
			return len(a) > len(b)
		}
		return len(a) > 0 && a[0] < b[0]
	})

	byHash := make(map[string]*Candidate, len(candidates))
	for i := range candidates {
		c := &candidates[i]
		if prev, ok := byHash[c.Hash]; !ok || c.TopicID > prev.TopicID {
			byHash[c.Hash] = c // prefer the most recent topic for a hash
		}
	}

	claimed := make(map[int64]bool)
	out := make([]Assignment, len(clusters))
	for _, idx := range order {
		items := sorted[idx]
		a := Assignment{Hash: SetHash(items), Transition: TransitionNew}
		if c, ok := byHash[a.Hash]; ok && !claimed[c.ThreadID] {
			attach(&a, c, 1, opts, claimed)
		} else if best, j := bestCandidate(items, candidates, claimed); best != nil && j >= opts.AttachThreshold {
			attach(&a, best, j, opts, claimed)
		}
		out[idx] = a
	}
	return out
}

func attach(a *Assignment, c *Candidate, j float64, opts Options, claimed map[int64]bool) {
	a.ThreadID, a.Jaccard, a.Transition = c.ThreadID, j, TransitionSurvived
	claimed[c.ThreadID] = true
	if opts.Inherit && j >= opts.InheritThreshold {
		a.Label = c.ThreadLabel
	}
}

// bestCandidate skips claimed threads; ties go to the most recent TopicID.
func bestCandidate(items []int64, cands []Candidate, claimed map[int64]bool) (*Candidate, float64)
```

Tests (`lineage_test.go`, package `lineage`):
- `TestSetHashIsOrderIndependent` — `{3,1,2}` and `{1,2,3}` equal; length 32; `{1,2,4}` differs.
- `TestJaccard` — table: identical → 1; disjoint → 0; `{1,2,3,4}` vs `{1,2,3,5}` → 0.6; both empty → 0.
- `TestAssignExactMatchInheritsLabel` — one candidate, same items → ThreadID, Jaccard 1, Label = ThreadLabel, Survived.
- `TestAssignNearMatchInheritsAboveThreshold` — 9 of 10 shared (J=0.818… < 0.9) attaches with `Label == ""`; 19 of 20 (J≈0.905) attaches with label.
- `TestAssignBelowAttachThresholdIsNew` — 2 of 10 shared → ThreadID 0, New.
- `TestAssignContestedThreadGoesToLargestCluster` — two clusters both best-match one candidate: the larger gets the thread, the smaller is New; result order matches input order regardless of processing order.
- `TestAssignInheritDisabledStillAttaches` — `Inherit: false`, exact match → ThreadID set, Label "".
- `TestAssignIsDeterministic` — same inputs twice → `reflect.DeepEqual`; clusters permuted → per-cluster results identical.
- `TestAssignPrefersMostRecentCandidateForHash` — two candidates with the same hash, different ThreadLabel; the higher TopicID's label is inherited.

**Verification — automated:**
- [x] `go test ./internal/lineage/... -v` passes with every test above present — **9 tests PASS, 0.137s**
- [x] `make check` passes — **0 lint issues, all packages ok**

**Verification — manual:**
- [x] Read `Assign` once more against spec "Matching rule" steps 1–3; the three branches map one-to-one. — **exact hash → bestCandidate ≥ attach → new; inherit gate inside attach()**

---

## Phase 2: Migration 14 DDL and storage

Delivers the two tables, `Topic` carrying lineage fields, `InsertTopicRun`
writing lineage and threads in its transaction, a candidates reader, and
orphan-thread cleanup in purge. After this phase every run creates threads
(all new) — a data foundation with no behaviour change yet.

**Files:**
- Modify: `internal/database/migrations.go` — version 14 constant, description, `migration14DDL` constant, `maxMigrationVersion`
- Modify: `internal/database/schema.sql` — append the DDL verbatim
- Modify: `internal/database/topic.go` — `Topic` fields, `InsertTopicRun`, `GetTopicsForRun`, `DeleteTopicRuns`, new `writeTopicLineage`, `GetLineageCandidates`
- Test: `internal/database/migration14_test.go` (new), `internal/database/topic_test.go`

**Key changes:**

`migrations.go`:
```go
migrationVersion14  = 14 // Add topic threads and per-topic lineage
maxMigrationVersion = migrationVersion14
// description: migrationVersion14: "add topic thread lineage tables and assign threads to existing topics"
// getMigrations(): migrationVersion14: migration14DDL,

// migration14DDL is pure CREATE IF NOT EXISTS so it is idempotent and its
// sqlite_master text matches schema.sql (TestMigration14MatchesSchemaFile).
// Lineage is a 1:1 side table rather than columns on topics because ALTER
// TABLE ADD COLUMN is neither idempotent nor text-stable against an inline
// CREATE. topic_threads has no FK to runs: threads outlive the runs that
// created them and are removed by purge once no lineage row points at them.
const migration14DDL = `CREATE TABLE IF NOT EXISTS topic_threads (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			first_seen_at DATETIME NOT NULL,
			last_seen_at  DATETIME NOT NULL,
			label         TEXT     NOT NULL,
			labeled_at    DATETIME NOT NULL
		);
		CREATE TABLE IF NOT EXISTS topic_lineage (
			topic_id     INTEGER PRIMARY KEY REFERENCES topics(id) ON DELETE CASCADE,
			thread_id    INTEGER NOT NULL REFERENCES topic_threads(id),
			set_hash     TEXT    NOT NULL,
			label_source TEXT    NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_topic_lineage_thread ON topic_lineage(thread_id);
		CREATE INDEX IF NOT EXISTS idx_topic_lineage_hash   ON topic_lineage(set_hash);`
```
In this phase migration 14 goes through the `default:` branch of
`applySpecificMigration` (DDL only). Phase 5 replaces that with a custom case.

`topic.go`:
```go
type Topic struct {
	ID          int64
	RunID       int64
	Label       string
	Score       float64
	ThreadID    int64  // 0 on input = open a new thread; always set after insert
	SetHash     string // computed on insert when empty
	LabelSource string // lineage.SourceGenerated (default) | lineage.SourceInherited
	ThreadIsNew bool   // transient: set by the pipeline, not persisted
}

// writeTopicLineage creates or updates the topic's thread and inserts its
// lineage row. Shared by InsertTopicRun and the migration 14 backfill so the
// two cannot disagree on what a thread is.
func writeTopicLineage(ctx context.Context, tx *sql.Tx, runAt time.Time, topic *Topic, items []int64) error {
	if topic.SetHash == "" {
		topic.SetHash = lineage.SetHash(items)
	}
	if topic.LabelSource == "" {
		topic.LabelSource = lineage.SourceGenerated
	}
	at := formatDatabaseTime(runAt)
	switch {
	case topic.ThreadID == 0:
		res, err := tx.ExecContext(ctx, `INSERT INTO topic_threads (first_seen_at, last_seen_at, label, labeled_at)
			VALUES (?, ?, ?, ?)`, at, at, topic.Label, at)
		// ... topic.ThreadID = LastInsertId; topic.ThreadIsNew = true
	case topic.LabelSource == lineage.SourceGenerated:
		_, err = tx.ExecContext(ctx, `UPDATE topic_threads SET last_seen_at = ?, label = ?, labeled_at = ? WHERE id = ?`,
			at, topic.Label, at, topic.ThreadID)
	default: // inherited
		_, err = tx.ExecContext(ctx, `UPDATE topic_threads SET last_seen_at = ? WHERE id = ?`, at, topic.ThreadID)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO topic_lineage (topic_id, thread_id, set_hash, label_source)
		VALUES (?, ?, ?, ?)`, topic.ID, topic.ThreadID, topic.SetHash, topic.LabelSource)
	return err
}
```
`InsertTopicRun` calls `writeTopicLineage(ctx, tx, run.CreatedAt, topic, items)`
immediately after the `topic_items` loop for each topic. A bogus `ThreadID`
fails on the `topic_lineage` FK, so a caller cannot silently attach to nothing.

```go
// GetTopicsForRun LEFT JOINs lineage so rows from before migration 14 still load.
SELECT t.id, t.run_id, t.label, t.score,
       COALESCE(l.thread_id, 0), COALESCE(l.set_hash, ''), COALESCE(l.label_source, '')
FROM topics t LEFT JOIN topic_lineage l ON l.topic_id = t.id
WHERE t.run_id = ? ORDER BY t.score DESC

// GetLineageCandidates returns every threaded topic in the `lookback` most
// recent runs created strictly before `before`, with ascending item IDs.
// `before` is the new run's CreatedAt for live use; the backfill passes each
// historical run's CreatedAt so it only sees that run's past.
func (db *DB) GetLineageCandidates(ctx context.Context, before time.Time, lookback int) ([]lineage.Candidate, error) {
	return lineageCandidates(ctx, db.conn, before, lookback)
}

// queryer is satisfied by *sql.DB and *sql.Tx. The backfill MUST read through
// its open transaction: SetMaxOpenConns(1) means a db.conn query while a tx
// is open blocks until busy_timeout and then fails.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func lineageCandidates(ctx context.Context, q queryer, before time.Time, lookback int) ([]lineage.Candidate, error) {
	rows, err := q.QueryContext(ctx, `
		WITH recent AS (
			SELECT id FROM topic_runs WHERE created_at < ? ORDER BY created_at DESC LIMIT ?
		)
		SELECT t.id, l.thread_id, th.label, l.set_hash, ti.item_id
		FROM topics t
		JOIN recent r        ON r.id = t.run_id
		JOIN topic_lineage l ON l.topic_id = t.id
		JOIN topic_threads th ON th.id = l.thread_id
		JOIN topic_items ti  ON ti.topic_id = t.id
		ORDER BY t.id, ti.item_id`, formatDatabaseTime(before), lookback)
	// group consecutive rows by t.id into one Candidate
}
```

`DeleteTopicRuns` becomes a transaction: the existing `DELETE FROM topic_runs`
followed by
`DELETE FROM topic_threads WHERE id NOT IN (SELECT thread_id FROM topic_lineage)`.
Return value unchanged (runs deleted).

Tests:
- `migration14_test.go`, mirroring `migration13_test.go` with
  `tblTopicThreads = "topic_threads"`, `tblTopicLineage = "topic_lineage"`:
  `TestMigration14TableExistsOnAFreshDatabase`, `TestMigration14IsRegistered`
  (asserts `maxMigrationVersion == migrationVersion14`, fragments, description),
  `TestMigration14IsIdempotent` (execute DDL 3×, one table each),
  `TestMigration14MatchesSchemaFile` (other DB: `items` stub + migration 13 SQL
  + migration 14 SQL; compare both tables' `sqlite_master.sql` via `normalizeSQL`),
  `TestMigration14DoesNotTouchNetwork` (DDL contains neither `http` nor `INSERT`).
- `topic_test.go`:
  - `TestInsertTopicRunOpensThreads` — two topics with `ThreadID 0`: after insert
    both have distinct non-zero `ThreadID`, `ThreadIsNew`, `SetHash` of length 32
    equal to `lineage.SetHash(items)`, `LabelSource == "generated"`;
    `topic_threads.label` equals each topic's label; `first_seen_at == last_seen_at`.
  - `TestInsertTopicRunAttachesInheritedTopic` — second run's topic with
    `ThreadID` from the first and `LabelSource: "inherited"`: thread count still 1,
    `last_seen_at` advanced, `label`/`labeled_at` unchanged.
  - `TestInsertTopicRunRelabelsThreadOnGeneratedSurvivor` — same but
    `LabelSource: "generated"` and a new label: thread `label` and `labeled_at` updated.
  - `TestInsertTopicRunRejectsUnknownThread` — `ThreadID: 9999` → error, and
    afterwards zero `topic_runs` rows (rollback).
  - `TestGetTopicsForRunIncludesLineage` — fields round-trip; a topic inserted by
    raw SQL without lineage loads with `ThreadID 0`, empty hash/source.
  - `TestGetLineageCandidates` — three runs at t, t+1h, t+2h; `before = t+3h,
    lookback 2` returns only runs 2–3's topics with ascending `Items`, correct
    `ThreadLabel` and `Hash`; `before = run2.CreatedAt, lookback 6` returns only run 1.
  - `TestDeleteTopicRunsRemovesOrphanThreads` — extend `TestDeleteTopicRuns`:
    the deleted run's thread is gone, the kept run's thread remains.

**Verification — automated:**
- [x] `go test ./internal/database/ -run 'Migration14|TopicRun|Lineage|TopicsForRun|DeleteTopicRuns' -v` passes — **16 PASS (5 migration14, 6 lineage, 4 pre-existing topic, DeleteTopicRuns extended)**
- [x] `go test ./internal/renderer/... ./internal/sitegroup/...` still pass (they call `InsertTopicRun` with `ThreadID 0`) — **ok, via make check**
- [x] `make check` passes — **0 lint issues after dropping migration 13's stale max-version pin (as migration 12's test did when 13 landed)**

**Verification — manual:**
- [x] `schema.sql` tail and `migration14DDL` are byte-identical apart from indentation. — **diff after whitespace collapse: identical; TestMigration14MatchesSchemaFile agrees**

---

## Phase 3: The pipeline inherits labels

Delivers the user-visible win: a second `topics` run over unchanged clusters
makes zero LLM calls and keeps its labels. Defaults come from `lineage`
constants; config plumbing is Phase 4.

**Files:**
- Modify: `internal/topics/pipeline.go` — `Pipeline` fields, lineage step, extracted `labelClusters`, summary log
- Test: `internal/topics/pipeline_test.go` (new) with a fake `Labeler`

**Key changes:**

```go
type Pipeline struct {
	db      *database.DB
	labeler Labeler
	// Lineage controls thread matching and label reuse; Lookback is how many
	// previous runs are candidates. NewPipeline sets the lineage defaults.
	Lineage  lineage.Options
	Lookback int
}

func NewPipeline(db *database.DB, labeler Labeler) *Pipeline {
	return &Pipeline{db: db, labeler: labeler, Lookback: lineage.DefaultLookback,
		Lineage: lineage.Options{AttachThreshold: lineage.DefaultAttachThreshold,
			InheritThreshold: lineage.DefaultInheritThreshold, Inherit: true}}
}
```

In `Generate`, build `run` **before** labelling (move the existing block up),
then after `filterByDiversity` and `GetItemTextsByIDs`:
```go
candidates, err := p.db.GetLineageCandidates(ctx, run.CreatedAt, p.Lookback)
if err != nil { return nil, nil, nil, fmt.Errorf("failed to load lineage candidates: %w", err) }
assignments := lineage.Assign(validClusters, candidates, p.Lineage)
topics, topicItemsMap, err := p.labelClusters(ctx, validClusters, assignments, itemsMap, concurrency)
```
Extract the goroutine fan-out (current `pipeline.go:85-136`) into
`labelClusters`; it keeps the semaphore, buffered channel and first-error-aborts
behaviour, and the goroutine body becomes:
```go
a := assignments[i]
topic := &database.Topic{Score: float64(len(c)), ThreadID: a.ThreadID, SetHash: a.Hash,
	ThreadIsNew: a.ThreadID == 0}
if a.Label != "" {
	topic.Label, topic.LabelSource = a.Label, lineage.SourceInherited
} else {
	label, err := p.getLabelForCluster(ctx, c, itemsMap) // unchanged; singletons still skip the LLM
	topic.Label, topic.LabelSource = label, lineage.SourceGenerated
}
```
After collection, before saving:
```go
logrus.Infof("Labeled %d topics: %d inherited, %d generated, %d new threads",
	len(topics), inherited, generated, newThreads)
```
`Generate` keeps its `//nolint:funlen`; `labelClusters` must fit cyclop 15
without one (it is the old loop moved, not new logic).

Tests (`pipeline_test.go`, package `topics`):
```go
type fakeLabeler struct{ mu sync.Mutex; calls int }
func (f *fakeLabeler) LabelCluster(_ context.Context, _ []string) (string, error) {
	f.mu.Lock(); defer f.mu.Unlock(); f.calls++; return fmt.Sprintf("label %d", f.calls), nil
}
func (f *fakeLabeler) ModelID() string { return "fake-llm" }
```
Fixture helper `newTopicsTestDB(t) (*database.DB, []int64)`:
`database.New(filepath.Join(t.TempDir(), "t.db"))` + `InitSchema()`;
`db.UpsertFeed(&database.Feed{URL: feedURL, FeedJSON: database.JSON("{}")})`;
nine items via `db.UpsertItem(&database.Item{FeedURL, GUID: "g%d", Title,
Link, PublishedDate: now, FirstSeen: sql.NullTime{Time: now, Valid: true},
ItemJSON: database.JSON("{}")})`, IDs via `db.GetItem(feedURL, guid).ID`;
embeddings via raw SQL on `db.GetConnection()`:
`INSERT INTO item_embeddings (item_id, model_id, dims, vector, source_hash, generator_version, computed_at) VALUES (?, 'fake-embed', 4, ?, 'h', 1, ?)`
with `database.EncodeVector(v)` and `time.Now().UTC().Format(time.RFC3339Nano)`.
Items 1–3 get one-hot e0, 4–6 e1, 7–9 e2 (dims 4). `Generate` is called with
`embedModel "fake-embed"`, window `now-1h..now+1h`, `threshold 0.7`,
`minItems 2`, `maxItems 0`, `concurrency 2`, `maxFeedRatio 0` (all items share
one feed, so the diversity filter must be off), `minDiversityCount 2`.

- `TestGenerateInheritsLabelsForUnchangedClusters` — embed only items 1–6.
  Run 1: `calls == 2`, both topics `LabelSource generated`, distinct `ThreadID`,
  `ThreadIsNew`. Run 2 (same pipeline, fresh `Generate`): `calls == 2` still,
  labels equal run 1's by `SetHash`, `LabelSource inherited`, same `ThreadID`s,
  `ThreadIsNew false`; `SELECT COUNT(*) FROM topic_threads` = 2.
- `TestGenerateOpensThreadForNewCluster` — run 1 with items 1–6; then embed
  7–9 and run 2: 3 topics, `calls == 3`, exactly one `ThreadIsNew`, thread count 3.
- `TestGenerateNoInheritRelabelsButKeepsThreads` — `p.Lineage.Inherit = false`;
  run 2 has `calls == 4`, both `LabelSource generated`, `ThreadID`s unchanged,
  and `topic_threads.label` now equals the new labels.
- `TestGenerateWithNoPriorRunsIsAllNew` — first ever run: every topic
  `ThreadIsNew`, `GetLineageCandidates` path returns empty without error.

**Verification — automated:**
- [x] `go test ./internal/topics/ -run TestGenerate -v` passes; the inheritance test fails first when `labelClusters` is made to ignore `a.Label` (prove it, then restore) — **4 PASS; with the inherit branch disabled the test failed on "an unchanged cluster must not call the LLM again", then restored**
- [x] `make check` passes — **0 issues; Generate no longer needs its funlen nolint**

**Verification — manual:**
- [x] Log output of a test run shows the `Labeled N topics: …` summary with plausible counts. — **run 1: "2 topics: 0 inherited, 2 generated, 2 new threads"; run 2: "2 inherited, 0 generated, 0 new threads"; new-cluster run: "3 topics: 2 inherited, 1 generated, 1 new threads"**

---

## Phase 4: Config, `--no-inherit`, JSON output, docs

Delivers the operator surface: three `topics:` keys, the flag, lineage fields
in `--json`, and documentation. `build` inherits config values and never
passes the flag.

**Files:**
- Modify: `internal/config/config.go` — constants, `TopicsConfig` fields, `LoadConfig`, `GetDefault`
- Modify: `cmd/root.go` — three `viper.SetDefault`
- Modify: `cmd/topics.go` — flag, pipeline wiring, JSON fields
- Modify: `feedspool.yaml.example`, `MANUAL.md`
- Test: `internal/config/topics_test.go` (new)

**Key changes:**

```go
// config.go — constants reference the leaf package so there is one number.
DefaultTopicsLineageLookback  = lineage.DefaultLookback
DefaultTopicsLineageThreshold = lineage.DefaultAttachThreshold
DefaultTopicsInheritThreshold = lineage.DefaultInheritThreshold

// TopicsConfig additions
LineageLookback  int     `mapstructure:"lineage_lookback"`
LineageThreshold float64 `mapstructure:"lineage_threshold"`
InheritThreshold float64 `mapstructure:"inherit_threshold"`

// LoadConfig
LineageLookback:  getIntWithDefault("topics.lineage_lookback", DefaultTopicsLineageLookback),
LineageThreshold: getFloat64WithDefault("topics.lineage_threshold", DefaultTopicsLineageThreshold),
InheritThreshold: getFloat64WithDefault("topics.inherit_threshold", DefaultTopicsInheritThreshold),
// GetDefault: the same three from the constants.
// root.go: viper.SetDefault for topics.lineage_lookback / lineage_threshold / inherit_threshold.
```

`cmd/topics.go`:
```go
var topicsNoInherit bool
topicsCmd.Flags().BoolVar(&topicsNoInherit, "no-inherit", false,
	"Label every cluster fresh instead of reusing the label of an unchanged topic")

pipeline := topics.NewPipeline(db, labeler)
pipeline.Lookback = cfg.Topics.LineageLookback
pipeline.Lineage = lineage.Options{
	AttachThreshold:  cfg.Topics.LineageThreshold,
	InheritThreshold: cfg.Topics.InheritThreshold,
	Inherit:          !topicsNoInherit,
}
```
`printTopicsJSON` adds per topic: `"thread_id": t.ThreadID`, `"set_hash": t.SetHash`,
`"label_source": t.LabelSource`, and `"transition": lineage.TransitionNew` when
`t.ThreadIsNew` else `lineage.TransitionSurvived`. Table output unchanged.

`feedspool.yaml.example`, inside `topics:` after `concurrency`:
```yaml
  lineage_lookback: 6                # Previous runs searched for the same topic
  lineage_threshold: 0.5             # Item-set Jaccard at/above which a cluster joins an existing thread
  inherit_threshold: 0.9             # Jaccard at/above which it also keeps the thread's label (no LLM call)
```
`MANUAL.md` `### topics`: add `--no-inherit` to Flags; add a short
"Threads and label inheritance" paragraph (hash → Jaccard → new; inherited
labels skip the LLM; `--json` fields); add the three keys to the YAML example;
extend Side effects with `topic_threads` and `topic_lineage`. In the Data
Model section add both tables next to `topic_runs`. Do not touch the
pre-existing `default_last` / `--min-items` drift (spec "NOT doing").

Tests (`internal/config/topics_test.go`, mirroring `embed_test.go`):
- `TestGetDefaultTopicsLineage` — `cfg.Topics.LineageLookback == 6`,
  `LineageThreshold == 0.5`, `InheritThreshold == 0.9`.
- `TestLoadConfigReadsTopicsLineage` — `viper.Reset()`; `viper.Set("topics.lineage_lookback", 3)`
  etc.; `LoadConfig()` returns them; unset keys fall back to defaults.

**Verification — automated:**
- [x] `go test ./internal/config/ -run Topics -v` passes — **3 PASS**
- [x] `make build && ./feedspool topics --help` lists `--no-inherit` — **shown with its help text**
- [x] `make check` passes — **0 issues**

**Verification — manual:**
- [x] `MANUAL.md` `topics` section reads coherently top to bottom; the YAML in MANUAL and `feedspool.yaml.example` agree on key names. — **read back after edit; both use lineage_lookback / lineage_threshold / inherit_threshold**

---

## Phase 5: Backfill lineage in migration 14, verify on real data

Delivers the upgrade story: the first `topics` run after upgrading already
inherits, because migration 14 replays lineage over every retained run using
the same `writeTopicLineage` and `lineage.Assign` as live runs.

**Files:**
- Create: `internal/database/topic_lineage_backfill.go`
- Modify: `internal/database/migrations.go` — `case migrationVersion14: return db.applyMigration14()`
- Test: `internal/database/migration14_test.go` — backfill tests and `rewindPastMigration14`

**Key changes:**

```go
// migrations.go — mirrors applyMigration11: schema in its own transaction,
// then the backfill, then INSERT OR IGNORE for the version because a
// concurrent serve/fetch can both enter this on first open and both do the
// work idempotently.
func (db *DB) applyMigration14() error {
	if err := db.applyMigrationSchemaStage(migrationVersion14, migration14DDL); err != nil {
		return err
	}
	logrus.Info("Assigning threads to existing topic runs")
	if err := db.BackfillTopicLineage(context.Background(), db.migrationBackfillProgress()); err != nil {
		return fmt.Errorf("failed to backfill topic lineage: %w", err)
	}
	_, err := db.conn.Exec("INSERT OR IGNORE INTO schema_migrations (version) VALUES (?)", migrationVersion14)
	return err
}
// applyMigrationSchemaStage is applyMigration11Schema generalised over
// (version, sql); applyMigration11Schema becomes a one-line call to it.
```

```go
// topic_lineage_backfill.go
//
// BackfillTopicLineage assigns a thread to every topic that has no lineage
// row, replaying runs oldest first so each run only sees its own past. It is
// idempotent (NOT EXISTS on topic_lineage) and makes no network call.
//
// Inherit is false: every historical label was LLM-generated, so each
// survivor re-labels its thread in turn and a thread ends up carrying the
// label of its most recent run -- nothing on the page changes at upgrade.
// (Les leans weakly toward the earliest label instead; flipping that is
// Inherit: true here, which freezes the first label.)
func (db *DB) BackfillTopicLineage(ctx context.Context, progress func(done, total int64)) error {
	runs, err := db.listTopicRunsAscending(ctx) // SELECT id, created_at FROM topic_runs ORDER BY created_at, id
	for i, run := range runs {
		if err := db.backfillRunLineage(ctx, run); err != nil {
			return fmt.Errorf("run %d: %w", run.ID, err)
		}
		if progress != nil { progress(int64(i+1), int64(len(runs))) }
	}
	return nil
}

func (db *DB) backfillRunLineage(ctx context.Context, run TopicRun) error {
	tx, err := db.conn.BeginTx(ctx, nil)
	defer rollbackUnlessDone(tx, "topic lineage backfill")
	// topics without lineage, largest first -- the same order Assign uses
	// live, so contested threads resolve identically.
	rows: SELECT t.id, t.label, t.score FROM topics t
	      WHERE t.run_id = ? AND NOT EXISTS (SELECT 1 FROM topic_lineage l WHERE l.topic_id = t.id)
	      ORDER BY t.score DESC, t.id ASC
	if len(topics) == 0 { return tx.Commit() }
	items: SELECT topic_id, item_id FROM topic_items WHERE topic_id IN (...) ORDER BY item_id
	candidates, err := lineageCandidates(ctx, tx, run.CreatedAt, lineage.DefaultLookback) // through tx!
	assignments := lineage.Assign(clusters, candidates, lineage.Options{
		AttachThreshold: lineage.DefaultAttachThreshold, InheritThreshold: lineage.DefaultInheritThreshold, Inherit: false})
	for i, topic := range topics {
		topic.ThreadID, topic.SetHash, topic.LabelSource = assignments[i].ThreadID, assignments[i].Hash, lineage.SourceGenerated
		if err := writeTopicLineage(ctx, tx, run.CreatedAt, topic, clusters[i]); err != nil { return err }
	}
	return tx.Commit()
}
```
The `IN (...)` list is built with `strings.Repeat("?,", n)` and bound args, the
way `GetItemsByIDs` does it. Reading candidates through `tx` is load-bearing:
with `SetMaxOpenConns(1)`, a `db.conn` query inside the transaction blocks.

Tests:
```go
func rewindPastMigration14(t *testing.T, db *DB) {
	t.Helper()
	execSQL(t, db, `DROP TABLE IF EXISTS topic_lineage`)
	execSQL(t, db, `DROP TABLE IF EXISTS topic_threads`)
	execSQL(t, db, `DELETE FROM schema_migrations WHERE version >= ?`, migrationVersion14)
}
```
- `TestMigration14BackfillsExistingRuns` — `setupTestDB`, `rewindPastMigration14`,
  seed items 1–8 with `seedItem`, then raw-SQL three runs at t, t+1h, t+2h:
  run1 {A: {1,2,3} "A1" score 3; B: {4,5} "B1" score 2}, run2 {A: {1,2,3} "A2";
  B: {4,5,6} "B2"}, run3 {A: {1,2,3} "A3"; C: {7,8} "C3"}. `RunMigrations()`.
  Assert: `GetMigrationVersion() == 14`; the three A topics share one thread
  whose `label == "A3"`, `labeled_at == run3`, `first_seen_at == run1`,
  `last_seen_at == run3`; the two B topics share a thread (J = 2/3 ≥ 0.5) with
  `label == "B2"`; C has its own thread; `COUNT(topic_threads) == 3`;
  `COUNT(topic_lineage) == 6`, all `label_source == 'generated'`, all hashes
  length 32.
- `TestMigration14BackfillIsIdempotent` — after the above, call
  `BackfillTopicLineage` again: thread and lineage counts unchanged, `labeled_at` unchanged.
- `TestMigration14BackfillLeavesLiveLineageAlone` — a run written by
  `InsertTopicRun` (which already has lineage) is untouched by a backfill.
- `TestMigration14BackfillOnEmptyDatabase` — no runs: no error, no threads.

Real-data smoke (manual; **copy first, every command migrates in place**):
```sh
mkdir -p /tmp/feedspool-78 && cp data/feeds.db /tmp/feedspool-78/spool.db
make build
./feedspool --database /tmp/feedspool-78/spool.db items --json | head -c 200 >/dev/null   # any command triggers the migration
sqlite3 /tmp/feedspool-78/spool.db "
  SELECT (SELECT COUNT(*) FROM topics), (SELECT COUNT(*) FROM topic_lineage), (SELECT COUNT(*) FROM topic_threads);
  SELECT th.label, COUNT(*) runs FROM topic_lineage l JOIN topic_threads th ON th.id = l.thread_id
  GROUP BY th.id ORDER BY runs DESC LIMIT 5;"
```

**Verification — automated:**
- [x] `go test ./internal/database/ -run Migration14 -v` passes — **9 PASS (5 DDL, 4 backfill); migration 11 tests still pass on the shared schema-stage helper**
- [x] `make check` passes — **0 issues**

**Verification — manual:**
- [x] On the copy: `topics` count equals `topic_lineage` count (every topic threaded). — **8079 / 8079**
- [x] Thread count is at most the spike's 122 (adjacent-only matching) and the five longest threads have 167 runs each, matching `make topic-lineage LAST=0`. — **121 threads (6-run lookback bridged one gap); 5 threads × 167 runs; 25 threads × 102 runs, same as the spike**
- [x] Migration on the 167-run copy finishes in seconds, with progress lines. — **1.3 s wall for 167 runs / 8,079 topics; announced as "Migrating database to schema version 14: …"**
- [x] `make topic-lineage DB=/tmp/feedspool-78/spool.db LAST=0` still runs unchanged. — **122 threads at adjacent-only matching, identical to the pre-migration run**
- [x] Optional, needs an LLM: `./feedspool --database /tmp/feedspool-78/spool.db topics --last 1w` logs `Labeled N topics: mostly inherited`, and a second run logs `0 generated`. — **run 1 (production config, gemini-2.5-flash via LiteLLM): "Labeled 69 topics: 67 inherited, 2 generated, 0 new threads"; run 2: "69 inherited, 0 generated, 0 new threads". Two LLM calls instead of 138.**
