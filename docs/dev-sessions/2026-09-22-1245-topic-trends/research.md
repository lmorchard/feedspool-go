# Research: topic read paths, CLI shape, time handling (slice 2)

Documentarian pass at `beee303` (slice 1 merged). Neutral; `file:line` refs.

## 1. Item dates and per-topic item loading

- `Item.EffectiveDate()` (`internal/database/models.go:89-97`): `PublishedDate.UTC()` if non-zero, else `FirstSeen.Time.UTC()` if valid, else zero. SQL twin: `julianday(COALESCE(published_date, first_seen))` (`item_repository.go:20`).
- `GetItemsByIDs(ids) (map[int64]*Item, error)` (`item_repository.go:176-219`): fills `ID, FeedURL, GUID, Title, Link, PublishedDate, FirstSeen, Content, Summary, Archived`; not `ItemJSON`. Unordered map. `nil, nil` on empty input.
- `renderer.BuildTopicsContext` (`internal/renderer/workflow.go:396-461`): `GetTopicsForRun` → `GetTopicItems` → `GetItemsByIDs`; optional per-site feed filter; `filterSingleTopicItems` (`:463-492`) drops missing/out-of-site items and applies the diversity rule; **`topicCopy := *topic; topicCopy.Score = len(filtered)`** (`:445-446`) — the only struct copy of `Topic`. DB errors return `nil, nil` silently.
- Feed grouping happens later in `FetchTopicMetadataAndFavicons` (`:497-560`) into `GroupsMap[topicID]`.
- `TopicsTemplateContext` (`internal/renderer/renderer.go:49-55`): `SiteChrome`, `Run`, `Topics`, `GroupsMap`, `Metadata`. Template reads `.Label`, `.Score`, `.ID`, `.Run.*` (`templates/topics.html:46-75`).

## 2. Reading topic runs and threads

- `Topic{ID, RunID, Label, Score, ThreadID, SetHash, LabelSource, ThreadIsNew}` (`internal/database/topic.go:21-34`).
- Readers: `GetLatestTopicRun` (`:109`), `GetTopicsForRun` (`:135`, LEFT JOIN lineage, score DESC), `GetTopicItems` (`:165`, `map[topicID][]itemID`), `GetLineageCandidates(before, lookback)` (`:305`), `listTopicRunsAscending` (`topic_lineage_backfill.go:57`, ID + CreatedAt only).
- **Missing:** run by ID; the run before a given run; all topics of a thread; any reader of `topic_threads.first_seen_at/last_seen_at/labeled_at`. Index `idx_topic_lineage_thread` exists (`schema.sql` ~`:167`).
- `lineage.Candidate{TopicID, ThreadID, ThreadLabel, Hash, Items}` (`internal/lineage/lineage.go:51-57`) — no run ID, score, or topic label.

## 3. Read-only CLI conventions

- Global `--json` (`cmd/root.go:55,60`) → `cfg.JSON` (`internal/config/config.go:79,205`); commands call `GetConfig()` (`root.go:147-152`).
- `items`/`show`: `--format table|json|csv`, `cfg.JSON` upgrades table→json (`cmd/items.go:112-114`, `cmd/show.go:149-155`); JSON via `json.NewEncoder(os.Stdout)` + `SetIndent("", "  ")`; tables via `tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)` with header + dashes row (`show.go:170-185`). Helpers are item-specific.
- `status`: no `--format`; `cfg.JSON` → indented encoder of a snake_case-tagged struct (`cmd/status.go:12-18,58`), else `fmt.Printf` lines.
- **No subcommands anywhere**: every `AddCommand` is on `rootCmd` (`cmd/topics.go:214` etc.).
- `topics` is generating (`cmd/topics.go:77-81`); `printTopicsJSON` (`:179-198`) emits `label, score, count, thread_id, set_hash, label_source, transition`; `printTopicsCLI` skips `Score < 2` (`:200-211`). forbidigo allowlist covers `cmd/topics.go` (`.golangci.yml`).

## 4. Time handling

- **No day/hour bucketing exists** in `internal/` or `cmd/`.
- `ParseTimeWindow` (`internal/database/time_utils.go:74-89`): windows are relative to now in UTC (`end = now`, `start = end - duration`), never calendar-aligned. `ParseDuration` handles `Nd`/`Nw`/`Nh` (`:129-151`).
- All timestamps stored UTC RFC3339Nano via `formatDatabaseTime` (`:69-71`): `topic_runs.*`, `topic_threads.*`, `items.published_date/first_seen`. `run.CreatedAt = time.Now().UTC()` (`internal/topics/pipeline.go:86-92`).

## 5. Consumers of `database.Topic`

- Non-test: `internal/topics/pipeline.go` (construction, `Generate` return), `internal/renderer/renderer.go:51-52`, `internal/renderer/workflow.go:397,442-446`, `cmd/topics.go:179,201`.
- Tests: `renderer/workflow_test.go`, `sitegroup/render_test.go`, `topics/pipeline_test.go`, database package tests.
- **`internal/api` does not expose topics** at all.
