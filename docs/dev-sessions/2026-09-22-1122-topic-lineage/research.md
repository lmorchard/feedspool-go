# Research: topics subsystem as it exists today

Documentarian pass over the worktree at HEAD `46466ec`, before any lineage
work. Neutral description with `file:line` references; no proposals.

## 1. Topics pipeline flow

**Signature** (`internal/topics/pipeline.go:33-37`):
`Generate(ctx, embedModel string, since, until time.Time, threshold float32, minItems, maxItems, concurrency int, maxFeedRatio float32, minDiversityCount int)`
returning `(*database.TopicRun, []*database.Topic, map[*database.Topic][]int64, error)`.
The pipeline struct holds only `db` and `labeler` (`pipeline.go:17-27`).

1. **Fetch embeddings.** `p.db.GetEmbeddingsForWindow(ctx, embedModel, since, until)` (`pipeline.go:39`; `internal/database/item_embedding.go:84-119`). Selects `item_embeddings` joined to `items`, filtered by `model_id` plus `effectiveDateSinceClause`/`effectiveDateUntilClause` (`item_repository.go:20-28`), ordered by `e.item_id ASC`. Zero embeddings returns `nil, nil, nil, nil` (`pipeline.go:43-46`).
2. **Cluster.** `clustering.Cluster(embeddings, threshold)` (`pipeline.go:49`; `internal/clustering/agglomerative.go:16-76`). All O(n²) pairs scored with `database.DotProduct` (`vector.go:69-80`); pairs with `sim >= threshold` merged by union-find, so clusters are connected components. Clusters come out of a `map[int][]int64` (`agglomerative.go:63-72`), so **cluster order is not deterministic**; within a cluster IDs follow input order (ascending `item_id`).
3. **Size filter.** `filterClusters` (`pipeline.go:189-203`): keep when `len >= minItems && (maxItems <= 0 || len <= maxItems)`.
4. **Diversity filter.** Only if `maxFeedRatio > 0` (`pipeline.go:56-61`), via `filterByDiversity` (`pipeline.go:205-253`) using `db.GetItemsByIDs` (`item_repository.go:176-215`). Clusters with `len < minDiversityCount` always kept; otherwise rejected when any one `FeedURL` has `count/len >= maxFeedRatio`.
5. **Item text for labels.** `p.db.GetItemTextsByIDs(allItemIDs)` (`pipeline.go:69`; `item_text.go:31-64`) reads `item_id, title, summary, body`.
6. **Build the run.** `TopicRun{CreatedAt: time.Now().UTC(), WindowStart, WindowEnd, EmbedModelID, LLMModelID: p.labeler.ModelID()}` (`pipeline.go:74-80`).
7. **Concurrent labelling** (`pipeline.go:85-136`). `concurrency <= 0` becomes 5. One goroutine per cluster gated by a semaphore; results on a buffered channel. Each result is `&database.Topic{Label, Score: float64(len(c))}` — **Score is cluster size**. First error aborts `Generate` (`pipeline.go:126-128`). Progress log every 10 clusters.
8. **Prompt input** (`getLabelForCluster`, `pipeline.go:153-187`). First 5 positions of the cluster; items with no `item_text` or empty title are skipped but consume a position. Each becomes `"Title: %s\nSnippet: %s"` with `Body` cut to 500 bytes. A singleton returns its own title (or `"Untitled Item"`) without calling the LLM.
9. **Sort** by `Score` descending (`pipeline.go:141-143`).
10. **Persist** with `p.db.InsertTopicRun(ctx, run, topics, topicItemsMap)` (`pipeline.go:145`).

**`Labeler` interface** (`internal/topics/llm.go:24-27`): `LabelCluster(ctx, titles []string) (string, error)` and `ModelID() string`.
- `OllamaLabeler` (`llm.go:39-115`): POST `{model, prompt, stream:false}` to `baseURL + "/api/generate"`, 30s timeout, optional bearer. Prompt at `llm.go:64-68`. Output trimmed and quote-stripped; empty output → `"Unknown Topic"`, empty input → `"Empty Topic"` (`llm.go:16-18`).
- `OpenAILabeler` (`internal/topics/openai.go:35-127`): same prompt as one `user` chat message to `/chat/completions`; URL suffix adjusted at `openai.go:85-90`.

**`db.InsertTopicRun`** (`internal/database/topic.go:31-90`), one transaction: `INSERT INTO topic_runs (created_at, window_start, window_end, embed_model_id, llm_model_id)` with `formatDatabaseTime`, sets `run.ID`; per topic `INSERT INTO topics (run_id, label, score)` and sets `topic.ID`/`RunID`; per item `INSERT INTO topic_items (topic_id, item_id)`. The map is keyed by `*Topic` pointer. Errors roll back via `rollbackUnlessDone`.

**`cmd/topics.go`:**
- Flags (`:197-215`): `--last`, `--since`, `--until`, `--model`, `--llm-model`, `--threshold`, `--min-items`, `--max-items`, `--concurrency`. `last` mutually exclusive with `since`/`until` (`:119-121`).
- `resolveTopicsParams` (`:118-169`): window via `database.ParseTimeWindow`; embed model `--model` → `cfg.Topics.EmbedModel` → `cfg.Embed.Model`; the rest via `resolveOption` (`:107-116`): flag if `Changed`, else non-zero config, else default.
- `Generate` call (`:69-73`) passes params plus `cfg.Topics.MaxFeedRatio` and `cfg.Topics.MinDiversityCount` (config-only, no flag).
- Labeler choice (`:52-59`): OpenAI if `BaseURL` contains `/v1` or `openai`, else Ollama. HTTP client timeout 120s.
- Output: `run == nil` → `--json` prints `[]`, table prints "No items embedded in this time window." (`:78-85`). `printTopicsJSON` (`:171-182`) emits `[{label, score, count}]`. `printTopicsCLI` (`:184-195`) prints header with window and models, then `■ label (score: %.1f, items: %d)`, skipping `Score < 2.0`.

## 2. Topic schema and its readers

DDL (`internal/database/schema.sql:131-151`), identical to migration 13 (`migrations.go:162-182`):
```sql
CREATE TABLE IF NOT EXISTS topic_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    window_start DATETIME NOT NULL,
    window_end DATETIME NOT NULL,
    embed_model_id TEXT NOT NULL,
    llm_model_id TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS topics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id INTEGER NOT NULL REFERENCES topic_runs(id) ON DELETE CASCADE,
    label TEXT NOT NULL,
    score REAL NOT NULL
);
CREATE TABLE IF NOT EXISTS topic_items (
    topic_id INTEGER NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
    item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    PRIMARY KEY (topic_id, item_id)
);
```
No secondary indexes. `PRAGMA foreign_keys = ON` in `database.New` (`db.go:57`).

Structs (`topic.go:9-28`): `TopicRun{ID, CreatedAt, WindowStart, WindowEnd, EmbedModelID, LLMModelID}`, `Topic{ID, RunID, Label, Score}`, `TopicItem{TopicID, ItemID}` (unused by queries).

| Reader | Location | Columns / behaviour |
|---|---|---|
| `GetLatestTopicRun` | `topic.go:94-117` | all `topic_runs` columns, `ORDER BY created_at DESC LIMIT 1`; no-rows detected by string compare on `err.Error()`, returns `nil, nil` |
| `GetTopicsForRun` | `topic.go:120-145` | `id, run_id, label, score WHERE run_id=? ORDER BY score DESC` |
| `GetTopicItems` | `topic.go:148-173` | `ti.topic_id, ti.item_id` joined on `t.run_id=?` → `map[topicID][]itemID` |
| `DeleteTopicRuns` | `topic.go:178-208` | `DELETE FROM topic_runs WHERE created_at < ?`; `keepLatest` adds `AND id NOT IN (latest)`. Children go by CASCADE |
| `cmd/purge.go` | `:301-307, :324, :339-354` | calls `DeleteTopicRuns(ctx, cutoff, true)`; dry-run counts with raw SQL |
| `renderer.resolveSiteTopics` | `internal/renderer/workflow.go:322-336` | `GetLatestTopicRun` then `BuildTopicsContext` |
| `renderer.BuildTopicsContext` | `workflow.go:396-461` | `GetTopicsForRun`, `GetTopicItems`, `GetItemsByIDs`; per-site filter on `FeedURL`; `filterSingleTopicItems` (`:463-492`) re-applies diversity; **copies each topic and overwrites `Score` with filtered item count** (`:445-447`) |
| `renderer.FetchTopicMetadataAndFavicons` | `workflow.go:497-560` | item `Link`, `FeedURL`; fills `GroupsMap[topicID]` |
| `renderer.RenderGlobalTopics` | `workflow.go:563-605` | same with no feed filter; `removeStaleTopicsFile` on no-topic paths |
| `sitegroup` | `internal/sitegroup/render.go:253-264` | `RenderGlobalTopics`, `hasTopics` into `SiteIndexContext` |
| template | `templates/topics.html:46,53-55,64-75` | `.Run.*`, `.ID` (anchors `#topic-{{.ID}}`), `.Label`, `.Score`, `index $.GroupsMap .ID`; context `TopicsTemplateContext` (`renderer.go:48-55`) |
| `scripts/topic-lineage` | `main.go:133-185` | raw `SELECT id, created_at FROM topic_runs`, then `GetTopicsForRun`/`GetTopicItems` |
| `cmd/build.go` | `:82-89` | no reads; calls `runTopics` |

**`SELECT *`:** none anywhere in `internal`, `cmd`, `scripts`. Every reader names columns and scans positionally.

## 3. Migration conventions

- Version constants `migrationVersion12 = 12`, `migrationVersion13 = 13`, `maxMigrationVersion = migrationVersion13` (`migrations.go:9-25`).
- `migrationDescriptions()` map (`:33-48`), shown via `announceMigration` (`migration_progress.go:27-31`).
- `getMigrations()` map (`:53-184`). 12 uses package constant `migration12DDL` (`:210-221`, rationale comment `:186-209`); 13 is inlined (`:162-182`). Both `IF NOT EXISTS` throughout.
- `applySpecificMigration` (`:288-316`) has custom Go cases for 2, 3, 4, 5, 6, 8, 9, 10, 11; others fall to `db.ApplyMigration(version, sql)` (`db.go:191-211`), which runs the SQL and records the version in one transaction.
- `RunMigrations` (`:224-285`): creates `schema_migrations`, reads `MAX(version)` (`db.go:156-169`), returns early at max, else applies `current+1..max` and errors `unknown migration version: N` on a gap.
- `InitSchema` (`db.go:115-129`) runs embedded `schema.sql` then `RunMigrations`. `schema.sql` records only version 1 (`schema.sql:40-46`), so a fresh DB also runs 2..13 — every migration must be idempotent. `IsInitialized` (`db.go:139-154`) also calls `RunMigrations`, so **every command migrates**.
- `schema.sql` is kept in step by appending DDL identical to the migration (`migrations.go:117-121, 190-193`; `schema.sql:64-68, 105-117`).

Tests:
- `migration12_test.go`: `TableExistsOnAFreshDatabase`, `IsRegistered`, `DoesNotEmbed` (forbids `http`, `embed_`, `INSERT INTO item_embeddings` in the DDL), `IsIdempotent` (execute 3×), `AddsNoDateIndex`, `MatchesSchemaFile` (`:98-130`: compares `sqlite_master.sql` of a `setupTestDB` DB against `setupTestDBForMigrations` + stub `items` + the migration SQL, via `normalizeSQL`, `expressions_test.go:27-29`).
- `migration13_test.go`: `TableExistsOnAFreshDatabase`, `IsRegistered` (asserts `maxMigrationVersion == migrationVersion13`), `IsIdempotent`, `MatchesSchemaFile` (`:80-112`), `TopicStorage` (`:114-162`).
- Helpers: `setupTestDB` (`test_helpers.go:9-33`), `setupTestDBForMigrations` (`migrations_test.go:13-36`), `setupOldDatabase` (`:43-84`). Rewind: `rewindPastMigration11` (`item_text_test.go:640-652`) deletes `version >= 11`; no generic rewind helper.

## 4. Topics configuration surface

- `TopicsConfig` (`internal/config/config.go:168-180`): `base_url`, `model`, `embed_model`, `api_key`, `concurrency`, `min_items`, `max_items`, `threshold`, `last`, `max_feed_ratio`, `min_diversity_count`.
- Constants (`config.go:45-46, 57-63`): `DefaultTopicsConcurrency=5`, `MinItems=7`, `MaxItems=100`, `Threshold=0.70`, `Last="1d"`, `DefaultTopicMaxFeedRatio=0.8`, `DefaultTopicMinDiversityCount=2`.
- `LoadConfig` (`config.go:248-260`): strings via `viper.GetString`; numerics via `getIntWithDefault`/`getFloat64WithDefault` (`:~10-24`); `GetDefault()` repeats defaults (`:318-328`).
- Viper defaults (`cmd/root.go:99-104`) for concurrency, min_items, max_items, threshold, last only.
- Env: `AutomaticEnv()` with no prefix (`root.go:106`); explicit `BindEnv` only for `embed.api_key` (`cmd/embed.go:84`) and `serve.api.token` (`cmd/serve.go:72`). None for topics.
- Precedence: flag → non-zero config → default via `resolveOption`. `max_feed_ratio`/`min_diversity_count` config-only.
- Render-side diversity knobs are separate (`config.go:105-106, 214-215`; `cmd/render.go:79-82, 206-207, 238-243`).
- `feedspool.yaml.example:60-71`: the `topics:` block. **Writes `default_last` while `LoadConfig` reads `topics.last`**; `max_feed_ratio`/`min_diversity_count` absent. (Pre-existing; out of scope here.)
- `MANUAL.md:856-893`: `topics` command; `--min-items` documented as default 5 (code says 7); `--max-items`, `--no-topics`, `skip_topics` undocumented. (Pre-existing.)
- `cmd/build.go`: `--no-topics` (`:48`); `skipTopics := buildNoTopics || cfg.Build.SkipTopics` (`:82`); calls `runTopics(cmd, nil)` (`:84`); failure is a warning and build continues (`:85`). Order fetch → embed → topics → render (`:65-91`). `buildCmd` defines no topics flags, so all `Changed()` checks are false and values come from config/defaults.

## 5. Existing tests around topics

- `internal/topics/llm_test.go`: one test, `TestOllamaLabeler` (`:14-71`), httptest server asserting path/model/stream and quote-stripping; nil titles → `"Empty Topic"`. **No tests for `OpenAILabeler`, `Pipeline`, `filterClusters`, `filterByDiversity`; no fake `Labeler` anywhere.** No `cmd/*_test.go`.
- `internal/database/topic_test.go`: `TestInsertTopicRun` (`:19-62`), `TestInsertTopicRunRollback` (`:64-96`, FK failure on item `-9999`), `TestGetTopicRunAndItems` (`:98-157`), `TestDeleteTopicRuns` (`:159-207`, 60-day-old run deleted by 30-day cutoff with `keepLatest`; CASCADE verified).
- Seeding: `setupTestDB`; `seedItem(t, db, guid, published)` (`item_embedding_test.go:33-~65`) returns `items.id`. Topic tests insert no embeddings.
- Renderer/sitegroup topic tests call `InsertTopicRun` directly: `workflow_test.go:520-597, 599-688, 690-711`; `sitegroup/render_test.go:541-~660`; `siteindex_test.go:85`.
- `clustering/agglomerative_test.go`: in-memory `[]*database.ItemEmbedding` fixtures via `unitVectorAt`/`mixedVector`; tests Mismatch, Empty, Single, Duplicates, Separation, Chaining (`:96-121`).

## 6. Time and identity conventions

- `formatDatabaseTime(t)` = `t.UTC().Format(time.RFC3339Nano)` (`time_utils.go:69-71`). `parseDatabaseTime` (`:33-67`) accepts several layouts. `topic_runs` times written with `formatDatabaseTime` (`topic.go:46`), read as strings and parsed (`topic.go:103-114`). `DeleteTopicRuns` and purge dry-run compare `created_at` as RFC3339Nano text.
- `items.id` is `INTEGER PRIMARY KEY AUTOINCREMENT` (`schema.sql:22`); natural key `UNIQUE(feed_url, guid)` (`:33`); upsert `ON CONFLICT(feed_url, guid) DO UPDATE ... RETURNING id` (`item_repository.go:35-47`) so re-fetch keeps the ID. FK'd from `item_text`, `item_embeddings`, `topic_items`, all CASCADE. It is the only item identity the clustering/topics code uses.
- GUID: generated as `sha256(link+title)` when absent (`models.go:251-255`). Link: used for `url_metadata` and CLI selectors.
- Hash IDs (`internal/ids/ids.go`): `FeedID` = first 8 hex of `sha256(url)`; `ItemID` = first 16 hex of `sha256(feedURL + "\n" + guid)`; computed not stored, so they survive a purge-and-refetch that reassigns `items.id` (`:1-6`). Duplicated as `FeedHashID`/`ItemHashID` in `pagination.go:17-33` with an agreement test.
- Content hash: `itemtext.SourceHash(title, summary, content)` = first 32 hex of `sha256(title + "\x00" + summary + "\x00" + content)` (`itemtext.go:38-44, 78-83`); stored in `item_text.source_hash`, copied to `item_embeddings.source_hash`; embedding staleness compares it. **Nothing in the topics tables records a hash.**
