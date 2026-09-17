# Render Topics in Site Spec

**Goal:** Generate a "Topics" page during static site generation based on the most recent `topic_runs` artifact in the database. Link to this page from the header of the site.

**Source:** User request.

## Current state
- `feedspool topics` clusters embedded items and persists a snapshot to `topic_runs`, `topics`, and `topic_items`.
- `feedspool render` generates a static site (`index.html`, `feeds/page-N.html`, `feeds/X.html`) from a time window, but does not read from or display topics.

## Desired end state
- When `feedspool render` runs, if there is a `topic_run` overlapping with the rendered time window (or just the latest one), it fetches the topics and their items.
- It generates a `topics.html` page in the output directory.
- The `topics.html` page lists the generated topics (by Label and Score), and underneath each topic, the list of items (`Item` details) associated with it.
- The `index.html` template has a link to `topics.html` in its header, maybe near the options menu or in a new navigation bar.

## Design decisions

- **Decision:** `render` does NOT invoke `topics` pipeline (no LLM generation).
  - **Why:** `render` is meant to be fast and deterministic. LLM generation takes minutes and costs money. `feedspool topics` must be run separately via cron/manually.
- **Decision:** Which `topic_run` to display?
  - **Why:** The latest successful run in the DB (`SELECT id FROM topic_runs ORDER BY created_at DESC LIMIT 1`). This is simplest and ensures the topics page always reflects the most recently computed trends.
- **Decision:** Page Generation.
  - **Why:** Add `topics.html` generation alongside `index.html` inside `internal/renderer/workflow.go`. Add `topics.html.tmpl` alongside `index.html.tmpl` in `internal/renderer/templates/`. Wait, the templates are currently `.html`, not `.tmpl`. e.g. `index.html`.
- **Decision:** Template Context.
  - **Why:** We will need to inject `LatestTopicRun *database.TopicRun`, `Topics []*database.Topic`, and `TopicItems map[int64][]database.Item` into a `TopicsContext` and pass it to `topics.html`. We will also add a `HasTopics bool` to `SiteChrome` so that `index.html` can conditionally display the "Trending Topics" link only if topics have been generated.

## Patterns to follow
- Existing renderer workflow: `internal/renderer/workflow.go`.
- Database access: Add `GetLatestTopicRun`, `GetTopicsForRun`, and `GetItemsForTopic` to `internal/database/topic.go`.
- Embedded templates: Add `topics.html` to `internal/renderer/templates/`.

## What we're NOT doing
- We are NOT calling LLMs during `render`.
- We are NOT modifying the clustering pipeline.
- We are NOT paginating the topics page (we assume the topics for a run will easily fit on one page).
