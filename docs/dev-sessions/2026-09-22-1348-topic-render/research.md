# Research: topics page rendering (slice 3)

Read directly at `e85ddb0` (slices 1 and 2 merged). Builds on the slice 2
research (`docs/dev-sessions/2026-09-22-1245-topic-trends/research.md` §1, §5).

## Render path

- Per site: `generateSite` → `resolveSiteTopics(db, config, chrome, feedURLs)`
  (`internal/renderer/workflow.go:265, 322-336`) → `GetLatestTopicRun` →
  `BuildTopicsContext(db, run, chrome, config, feedURLs)` (`:396-461`) →
  `FetchTopicMetadataAndFavicons` (`:497-560`) → `writeOrRemoveTopicsFile`.
- Global (multi-site): `RenderGlobalTopics(config, chrome)` (`:563-605`), same
  path with `allowedFeedURLs = nil`; called from `internal/sitegroup/render.go:253`.
- `BuildTopicsContext` filters each topic's items to the site's feeds and the
  diversity rule, then copies each surviving topic with `Score = len(filtered)`
  (`:442-449`), keeping DB score-DESC order. It returns the filtered items as
  `map[topicID][]*Item`.
- `TopicsTemplateContext{SiteChrome, Run, Topics, GroupsMap, Metadata}`
  (`internal/renderer/renderer.go:48-55`).

## Template and script

- `internal/renderer/templates/topics.html`: header box with meta line and
  `.topics-index` pills (`:41-58`), then one `<details class="topic-group-container"
  id="topic-{{.ID}}">` per topic with `.topic-summary` (arrow, `<h2>` label,
  `.topic-badge` "N items", `↑ Top` link) and feed groups (`:62-140`).
- Anchors use the per-run topic ID (`#topic-{{.ID}}`), which changes every run.
- `internal/renderer/assets/js/topic-navigator.js`: opens a `<details>` on
  `a[href^="#topic-"]` clicks and on `#topic-` hashchange; collapses on `↑ Top`.
- Template funcs live in `internal/renderer/templates.go:66-90`
  (`stripHTML`, `iframeContent`).
- Styles: `internal/renderer/assets/css/feed.css:150-340` (header box, pills,
  cards, `.topic-badge`); colours in `css/variables.css` with a dark-mode block.

## Dependencies

- `internal/topics` depends on database, lineage, trends, clustering, config,
  httpclient, embed; it does not import renderer, so renderer → topics adds no
  cycle.
- `topics.LoadTrends(ctx, db, run, topics, topicItems map[int64][]int64)`
  (`internal/topics/trends.go`) diffs against the thread's previous members,
  loaded unfiltered by `GetPreviousThreadItems`.

## Tests

- `internal/renderer/workflow_test.go:520` (page renders), `:599` (per-feed-list
  filtering), `:690` (stale file removed); `internal/sitegroup/render_test.go:541`
  (global + per-site pages); `siteindex_test.go:85` (HasTopics link). All
  build runs with `InsertTopicRun`.
