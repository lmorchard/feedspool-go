# Implementation Plan (Render Topics)

## Phase 1: Database retrieval methods
**Goal:** Add methods to `internal/database/topic.go` to fetch the latest topics run and its associated items.

**Files:**
- `internal/database/topic.go`: Add `GetLatestTopicRun() (*TopicRun, error)`, `GetTopicsForRun(runID int64) ([]*Topic, error)`, and `GetTopicItems(runID int64) (map[int64][]int64, error)`.
- `internal/database/topic_test.go`: Add tests for retrieving the data after inserting it.

**Key changes:**
- `GetLatestTopicRun`: `SELECT * FROM topic_runs ORDER BY created_at DESC LIMIT 1`
- `GetTopicsForRun`: `SELECT * FROM topics WHERE run_id = ? ORDER BY score DESC`
- `GetTopicItems`: returns a map of `topic_id` to a slice of `item_id`s for that run.

**Verification - automated:**
- [x] `make test` — **Passed!**

## Phase 2: Template generation and Context Updates
**Goal:** Inject topic data into the rendering pipeline and write `topics.html`.

**Files:**
- `internal/renderer/renderer.go`: Update `SiteChrome` struct with `HasTopics bool`. Add `TopicsTemplateContext` struct.
- `internal/renderer/workflow.go`: 
  - Update `generateSite` to fetch the latest topic run.
  - Update `createTemplateContext` to populate `HasTopics`.
  - Add `renderTopicsFile` function.
- `internal/renderer/templates.go`: Register `topics.html` in `LoadDefaultTemplateByName`.
- `internal/renderer/templates/topics.html` (new): Create the new HTML template.
- `internal/renderer/templates/index.html`: Add a link to `topics.html` if `HasTopics` is true.
- `internal/renderer/templates/feed.html`: Add the same link in the header.

**Key changes:**
```go
// internal/renderer/renderer.go
type TopicsTemplateContext struct {
    SiteChrome
    Run        *database.TopicRun
    Topics     []*database.Topic
    ItemsMap   map[int64][]*database.Item // Map of TopicID to slice of Items
    Metadata   map[string]*database.URLMetadata
    FeedFavicon map[string]string
}
```

- In `workflow.go`, fetch the latest run. If found, fetch topics, topic items, and then call `db.GetItemsByIDs()` to get the actual `Item` structs for rendering. Build the `TopicsTemplateContext` and execute the `topics.html` template.

**Verification - automated:**
- [x] `make test` — **passed!**
- [x] `make check` — **passed!**

**Verification - manual:**
- [x] Eyeball `topics.html` template to ensure it matches the styling of `index.html`. — **matched standard card/list layouts, styling looks good!**
- [x] Run `feedspool render` on the smoke DB and verify `topics.html` is generated successfully. — **It rendered perfectly.**
