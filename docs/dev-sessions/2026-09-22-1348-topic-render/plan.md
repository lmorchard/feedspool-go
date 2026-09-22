# Topics Page Trends — Implementation Plan

**Goal:** Render slice 2's trends on `topics.html`: topics grouped by status
(new → growing → fading → steady → quiet), status dots and badges, a stats
strip and an SVG sparkline per card, thread-ID permalinks.

**Approach:** Trends are computed at render time through `topics.LoadTrends`,
extended with an optional feed filter so per-site pages count only their own
items on both sides of the diff. Grouping, sorting and the tally happen in Go
into new template-context fields; the template only iterates. Visual values are
ported from `tmp/topic-render-mockup/index.html`.

**Tech stack:** Go `html/template`, embedded assets, vanilla custom element
(`topic-navigator.js`). `make check` = format → lint → test.

Shared vocabulary (defined where noted, used everywhere):
- `trends.StatusOrder() []string` — `[new, growing, fading, steady, quiet]` (Phase 1)
- `topics.LoadTrendsForFeeds(ctx, db, run, topicList, topicItems, allowedFeeds map[string]bool) (map[int64]trends.Trend, error)` (Phase 1)
- `renderer.TopicStatusGroup{Status string; Topics []*database.Topic}` (Phase 2)
- `TopicsTemplateContext` gains `Trends map[int64]trends.Trend`, `StatusGroups []TopicStatusGroup`, `StatusTally string` (Phase 2)
- `groupTopicsByStatus(topicList []*database.Topic, tr map[int64]trends.Trend) []TopicStatusGroup` (Phase 2)
- `statusTally(groups []TopicStatusGroup) string` (Phase 2)
- Template funcs `sparkline([]int) template.HTML`, `trendDelta(trends.Trend) string`, `trendDeltaClass(trends.Trend) string` (Phase 2)
- **Revised in Phase 2:** `trendDelta(last, prior int)` and `trendDeltaClass(last, prior int)` take the two counts (gocritic hugeParam, and a template cannot take the address of a map value). Templates call `trendDelta $tr.Last24h $tr.Prior24h`.

One commit per phase: `Phase N: <name>`. TDD by default; Phase 3's CSS and
script are covered through rendered-HTML assertions plus a manual visual check.

---

## Phase 1: Status order and feed-filtered trends

**Files:**
- Modify: `internal/trends/trends.go` — `StatusOrder`
- Modify: `internal/topics/trends.go` — `LoadTrendsForFeeds`; `LoadTrends` delegates
- Test: `internal/trends/trends_test.go`, `internal/topics/trends_test.go`

**Key changes:**

```go
// StatusOrder is the order statuses are presented in: what just appeared,
// what is moving, then what is not. A function so it cannot be mutated.
func StatusOrder() []string {
	return []string{StatusNew, StatusGrowing, StatusFading, StatusSteady, StatusQuiet}
}
```

```go
// LoadTrends computes trends over every item. See LoadTrendsForFeeds.
func LoadTrends(ctx, db, run, topicList, topicItems) (map[int64]trends.Trend, error) {
	return LoadTrendsForFeeds(ctx, db, run, topicList, topicItems, nil)
}

// LoadTrendsForFeeds is LoadTrends restricted to items from allowedFeeds, on
// both sides of the diff: a per-site page must not count another site's items
// as present now or as dropped since last run. nil means no restriction.
// With a restriction, members whose item row is gone are ignored -- their feed
// cannot be known. Without one they still count, as before.
func LoadTrendsForFeeds(ctx context.Context, db *database.DB, run *database.TopicRun,
	topicList []*database.Topic, topicItems map[int64][]int64, allowedFeeds map[string]bool,
) (map[int64]trends.Trend, error)
```
Body: as today, but fetch items for the union of current IDs and previous IDs
(previous comes from `GetPreviousThreadItems`, already loaded before items), then
for each topic build `ids` and `prevIDs` through `keep(id)`:
```go
keep := func(id int64) bool {
	if allowedFeeds == nil {
		return true
	}
	item, ok := items[id]
	return ok && allowedFeeds[item.FeedURL]
}
```
`in.ItemIDs` and `in.PrevItemIDs` are the kept IDs; `in.Items` is built from kept
IDs that have rows (unchanged rule).

Tests:
- `TestStatusOrder` (trends) — equals the five constants in order; mutating
  the returned slice does not affect a second call.
- `TestLoadTrendsForFeedsFiltersBothSides` (topics) — seed a second feed
  (`db.UpsertFeed` + `db.UpsertItem` for two items on `https://other.example/feed`);
  run 1 via `db.InsertTopicRun` with a topic of {a1, a2, o1}; run 2 at +1h with
  the same `ThreadID` and {a1, a3, o2} (a3 from `ids[2]`). With
  `allowedFeeds = {testFeedURL}`: `NewItems 1` (a3), `DroppedItems 1` (a2),
  `DistinctFeeds 1`, `sum(Daily) == 2`. With `nil`: `NewItems 2`,
  `DroppedItems 2`, `DistinctFeeds 2`, `sum(Daily) == 3`.
- Existing `LoadTrends` tests keep passing unchanged.

**Verification — automated:**
- [x] `go test ./internal/trends/ ./internal/topics/ -run 'StatusOrder|LoadTrends' -v` passes — **5 PASS (new two-feed test plus the three existing LoadTrends tests)**
- [x] `make check` passes — **0 issues after extracting loadThreadHistory (cyclop)**

**Verification — manual:**
- [x] `keep` is applied to both `ids` and `prevIDs`. — **keep(topicItems[t.ID]) and keep(p); the two-feed test fails either side without it**

---

## Phase 2: Renderer context, grouping, and template funcs

**Files:**
- Modify: `internal/renderer/renderer.go` — context fields, `TopicStatusGroup`
- Create: `internal/renderer/topic_trends.go` — `groupTopicsByStatus`, `statusTally`, `sparklineSVG`, `trendDelta`, `trendDeltaClass`
- Modify: `internal/renderer/templates.go` — register the three funcs
- Modify: `internal/renderer/workflow.go` — `BuildTopicsContext` loads trends and fills the fields
- Test: `internal/renderer/topic_trends_test.go` (new)

**Key changes:**

```go
type TopicStatusGroup struct {
	Status string            // "" only when trends could not be loaded
	Topics []*database.Topic
}
// TopicsTemplateContext additions:
Trends       map[int64]trends.Trend // topic ID -> trend
StatusGroups []TopicStatusGroup     // non-empty groups in trends.StatusOrder
StatusTally  string                 // "16 new · 4 growing · …", zero counts omitted
```

```go
// groupTopicsByStatus buckets topics by trend status in trends.StatusOrder,
// omitting empty groups, and sorts each group by last 24h desc, then item
// count (Score) desc, then label -- the order chosen in the mockup.
func groupTopicsByStatus(topicList []*database.Topic, tr map[int64]trends.Trend) []TopicStatusGroup

func statusTally(groups []TopicStatusGroup) string // "N status" joined by " · "

// sparklineSVG draws one bar per day: 6px wide, 2px gap, 18px tall, scaled to
// the topic's own peak with a 1px minimum; the last bar gets class "last".
// Built with fmt from integers only, so it is safe to return as template.HTML.
func sparklineSVG(daily []int) template.HTML

func trendDelta(t trends.Trend) string      // "▲2", "▼3", or "·"
func trendDeltaClass(t trends.Trend) string // "delta-up", "delta-down", or ""
```
SVG shape (fixed attribute order so tests can match it):
```html
<svg class="spark-svg" width="W" height="18" role="img" aria-label="items per day: 0, 2, 5"><title>0, 2, 5 per day, oldest first</title><rect x="0" y="17" width="6" height="1" rx="1"/>…<rect class="last" …/></svg>
```
`W = len*8 - 2`; empty input → `""`.

In `BuildTopicsContext`, after `cleanTopicList` is built:
```go
ids := make(map[int64][]int64, len(topicItemSlices))
for topicID, slice := range topicItemSlices {
	for _, item := range slice {
		ids[topicID] = append(ids[topicID], item.ID)
	}
}
topicTrends, err := topics.LoadTrendsForFeeds(context.Background(), db, run, cleanTopicList, ids, allowedSet)
if err != nil {
	logrus.WithError(err).Warn("Could not compute topic trends; rendering topics without them")
	ctx.StatusGroups = []TopicStatusGroup{{Topics: cleanTopicList}}
} else {
	ctx.Trends = topicTrends
	ctx.StatusGroups = groupTopicsByStatus(cleanTopicList, topicTrends)
	ctx.StatusTally = statusTally(ctx.StatusGroups)
}
```
(`ctx` here is the `*TopicsTemplateContext` being returned; build it before
this block.) Passing `allowedSet` makes the global page (`nil`) unfiltered and
per-site pages filtered.

Tests (`topic_trends_test.go`, package `renderer`):
- `TestGroupTopicsByStatus` — six topics over statuses {quiet, new, growing,
  growing, fading, new} with chosen `Last24h`/`Score`: groups come back in
  `new, growing, fading, quiet` order (no `steady` group); within `growing`,
  higher `Last24h` first; ties broken by `Score` then label.
- `TestStatusTally` — the groups above → `"2 new · 2 growing · 1 fading · 1 quiet"`.
- `TestSparklineSVG` — `[0, 2, 4]`: contains `width="22"`, three `<rect`, last
  rect has `class="last"` and `height="18"`, first has `height="1"` (minimum),
  `aria-label="items per day: 0, 2, 4"`; `nil` → `""`; `[0,0]` → two 1px bars.
- `TestTrendDelta` — last/prior 5/2 → `"▲3"`, `"delta-up"`; 1/4 → `"▼3"`,
  `"delta-down"`; 2/2 → `"·"`, `""`.

**Verification — automated:**
- [x] `go test ./internal/renderer/ -run 'GroupTopicsByStatus|StatusTally|SparklineSVG|TrendDelta' -v` passes — **4 PASS**
- [x] existing renderer and sitegroup topic tests still pass (template unchanged yet) — **both packages ok**
- [x] `make check` passes — **0 issues**

**Verification — manual:**
- [x] `BuildTopicsContext` passes the site's `allowedSet`, not `nil`, on per-site pages. — **addTopicTrends(db, run, ctx, topicItemSlices, allowedSet); allowedSet is nil only when allowedFeedURLs is nil (global page)**

---

## Phase 3: Template, styles, and navigator

**Files:**
- Modify: `internal/renderer/templates/topics.html`
- Modify: `internal/renderer/assets/css/variables.css` — status colours
- Modify: `internal/renderer/assets/css/feed.css` — new topic-page rules
- Modify: `internal/renderer/assets/js/topic-navigator.js` — `#thread-` anchors
- Test: `internal/renderer/workflow_test.go`, `internal/sitegroup/render_test.go`

**Key changes:**

Anchor helper in the template (topics without a thread keep a topic anchor):
```
{{define "anchor"}}{{if .ThreadID}}thread-{{.ThreadID}}{{else}}topic-{{.ID}}{{end}}{{end}}
```

Meta line: append `{{if .StatusTally}} — {{.StatusTally}}{{end}}`.

Pills, replacing the flat `range .Topics`:
```
{{range .StatusGroups}}
<div class="pill-group">
  {{if .Status}}<div class="pill-group-label"><span class="dot st-bg-{{.Status}}"></span>{{.Status}} · {{len .Topics}}</div>{{end}}
  {{range .Topics}}
  <a href="#{{template "anchor" .}}" class="topic-pill{{with index $.Trends .ID}}{{if eq .Status "quiet"}} is-quiet{{end}}{{end}}">
    {{with index $.Trends .ID}}<span class="dot st-bg-{{.Status}}"></span>{{end}}
    <span class="topic-pill-label">{{.Label}}</span>
    <span class="topic-pill-count">{{.Score | printf "%.0f"}}</span>
  </a>
  {{end}}
</div>
{{end}}
```

Cards: wrap the existing card loop in `{{range .StatusGroups}}`, emitting
`<div class="status-heading"><span class="st st-{{.Status}}">{{.Status}}</span> {{len .Topics}} topics</div>`
when `.Status` is set. Card element becomes
`<details class="topic-group-container" id="{{template "anchor" .}}">`. In the
summary, add `<span class="st st-{{$tr.Status}}">{{$tr.Status}}</span>` before
the `<h2>` and wrap the right side:
```
{{$tr := index $.Trends .ID}}
<div class="topic-summary-right">
  {{if $tr.Status}}
  <span class="topic-stats">
    <span><b>{{$tr.DistinctFeeds}}</b> feeds</span>
    <span title="last 24h vs the 24h before"><b>{{$tr.Last24h}}</b> today <span class="{{trendDeltaClass $tr}}">{{trendDelta $tr}}</span></span>
    {{if and $tr.NewItems (ne $tr.Status "new")}}<span><b>+{{$tr.NewItems}}</b> since last run</span>{{end}}
  </span>
  {{sparkline $tr.Daily}}
  {{end}}
  <a href="#top" class="back-to-top" title="Back to top">↑ Top</a>
</div>
```
(`index` on a missing key yields the zero `Trend`, whose empty `Status` hides
the badge and strip, which is the no-trends fallback.) The feed-group body inside each card
is unchanged; it keeps using `index $.GroupsMap .ID`, and `$` still refers to
the root context inside nested `range`.

CSS, ported from the mockup's candidate-design block:
- `variables.css`: `--st-new: #8e44ad; --st-growing: #27ae60; --st-fading: #e67e22; --st-steady: #7f8c8d; --st-quiet: #bdc3c7;` and dark-mode `--st-quiet: #5a6270;`.
- `feed.css`: `.st` + `.st-<status>` badges (quiet uses primary text colour);
  `.dot` + `.st-bg-<status>` backgrounds; `.topic-pill.is-quiet { opacity: .55 }`;
  `.topics-index` becomes `display: block` with `.pill-group` flex-wrap rows,
  `.pill-group + .pill-group { margin-top: .9rem }`, and `.pill-group-label` as
  a full-width header line (`flex-basis: 100%`, .72rem bold uppercase,
  `--text-secondary`); `.status-heading`; `.topic-summary-right` flex with 1rem
  gap; `.topic-stats` (.8rem, secondary, bold values in primary);
  `.delta-up`/`.delta-down` in growing/fading colours; `.spark-svg rect` tertiary
  fill, `rect.last` primary.

`topic-navigator.js`: replace both `#topic-` prefix checks with a helper
`isTopicAnchor(hash)` that accepts `#thread-` and `#topic-` (the latter for
threadless topics), used by the click delegate selector
`a[href^="#thread-"], a[href^="#topic-"]` and by `handleHashChange`.

Tests (update, then run):
- `TestExecuteWorkflowRendersTopicsPage`: the fixture's topics now have threads
  (InsertTopicRun opens them). Add expected snippets: `id="thread-`,
  `href="#thread-`, `class="pill-group-label"`, `class="status-heading"`,
  `class="st st-new"` (both threads opened now), `class="spark-svg"`,
  `class="topic-stats"`, and the tally ` — 2 new` in the meta line. Keep the
  existing snippets that still apply (`topic-pill`, `topic-group-container`,
  `topic-summary`, `topic-badge`, `back-to-top`, `AI &amp; ML`).
- `TestExecuteWorkflowFiltersTopicsPerFeedList`: add an assertion that a
  per-site page's tally counts only topics that survived that site's filter.
- `TestRenderAllGeneratesTopLevelTopicsAndPerSiteTopics`: assert `id="thread-`
  appears in both the global and a per-site `topics.html`.
- New `TestTopicsPageThreadlessTopicKeepsTopicAnchor` in `workflow_test.go`:
  insert a topic by raw SQL with no lineage row (ThreadID 0 on read) and assert
  its card has `id="topic-<id>"`.

**Verification — automated:**
- [x] `go test ./internal/renderer/ ./internal/sitegroup/ -v -run 'Topic'` passes — **both packages ok; the three updated tests failed first against the old template**
- [x] `make check` passes — **0 issues; navigator passes node --check. Added beyond the mockup: `.topic-stats` hidden below 700px so card headers don't overflow on phones (flagged to Les)**

**Verification — manual:**
- [ ] Rendered page matches the locked mockup (Phase 4 smoke).

---

## Phase 4: Real-data render and docs

**Files:**
- Modify: `MANUAL.md` — the `render` section never mentions the topics page;
  add a "Topics page" paragraph after its **Side effects** line: written when a
  topic run exists, grouped by status (link to `topics latest` for the
  definitions), per-site pages count only their own feeds, and `#thread-N`
  anchors are stable across rebuilds. Note that a custom `--templates`
  directory keeps its old `topics.html` until re-extracted; the new context
  fields are additive, so it still renders.

Smoke (**copy first; every command migrates in place**):
```sh
./feedspool --database /tmp/feedspool-78r/spool.db render --output /tmp/feedspool-78r/site
open /tmp/feedspool-78r/site/topics.html
```

**Verification — automated:**
- [!] `grep -c 'class="spark-svg"' /tmp/feedspool-78r/site/topics.html` equals the topic count from `topics latest --json | jq '.topics|length'` — **DOES NOT HOLD, and the check was wrong, not the work: 62 sparklines vs 71 topics because the renderer's pre-existing diversity filter drops 9 topics ("Render filtered out 9 topic(s)"). Sparklines == rendered cards == 62, which is the property that matters.**
- [x] every `href="#thread-N"` in the page has a matching `id="thread-N"` — **62 links, 62 anchors, 0 unmatched**
- [x] `make check` passes — **0 issues**

**Verification — manual:**
- [ ] Les compares the rendered page with the mockup (groups and header lines, colours, stats, sparklines, quiet last) in light and dark mode.
- [ ] Clicking a pill opens and scrolls to its card; `↑ Top` collapses it; reloading with a `#thread-N` hash opens that card.
- [x] MANUAL text reads coherently. — **"Topics page" paragraph under `render`, linking to `topics latest` for definitions**

---

## Follow-up: narrow-viewport fixes (requested by Les during review)

- [x] Card header is always one line: title truncates with an ellipsis; badges, sparkline and `↑ Top` never shrink or wrap. — **all 62 headers 51px tall at 390px wide (was 2–3 lines)**
- [x] Below 700px: the status badge in each card header becomes a coloured dot (text kept for screen readers); the count badge drops the word "items". — **phone screenshot checked**
- [x] Below 700px: the header's options trigger shows only ⚙ (`aria-label="Options"` kept); applied to index, feed and topics templates. — **phone screenshot checked**
- [x] Header nav link reads "Trending" instead of "Trending Topics" on every page (index, feed, topics, site index); page heading and <title> unchanged. — **8 occurrences across 4 templates and 3 tests; phone screenshot checked**
- [x] Page test now fails on raw template text in `topics.html`. — **added after a mangled edit leaked `| printf` into every card while all snippet checks still passed; proven to fire on a mangled template**
- [x] Growing/fading require a margin of 2 (`trends.GrowthMargin`); quiet now means zero in both windows explicitly; the stats-strip arrow is coloured only at the margin. — **Les's call after reviewing how fast statuses move. Production copy: 16 new · 1 growing · 7 fading · 11 steady · 36 quiet (was 3 growing · 13 fading · 1 steady)**
- [x] Growth margin is configurable: `topics.growth_margin` (default 2), `topics latest --growth-margin`, `render --topic-growth-margin`; `build` and directory mode read config. `LoadTrends` takes `TrendOptions{AllowedFeeds, GrowthMargin}` (replaces `LoadTrendsForFeeds`). — **production copy: default → 1 growing · 7 fading; margin 1 → 4 growing · 14 fading, matching the pre-margin numbers, through both `topics latest` and `render`**
- [x] Change arrow coloured by status (`trendDeltaClass(status)`), so it cannot disagree with the badge under any margin.
- [x] Card-header status badge is a coloured dot at every width (Les: the word repeated in every header was redundant under the group heading). — **desktop and phone screenshots checked**
- [x] Group headings say "1 topic" / "N topics". — **fixed after it showed "1 TOPICS" in a screenshot; page test asserts the plural**
