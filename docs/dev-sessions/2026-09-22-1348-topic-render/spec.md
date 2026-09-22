# Topics Page Trends Spec (issue #78, slice 3)

**Goal:** Show slice 2's trend signals on the rendered `topics.html` — topics
grouped by status, with a status colour, an SVG sparkline and a stats strip
per topic — and give each topic a permalink that survives rebuilds.

**Source:** [#78](https://github.com/lmorchard/feedspool-go/issues/78) §5; the
interactive mockup (`tmp/topic-render-mockup/`, gitignored) iterated with Les on
2026-09-22.

## Current state

`topics.html` lists the latest run's topics in size order as pills and
collapsible cards, anchored on the per-run topic ID (`research.md`). Trends
exist only on the CLI (`topics latest`, `topics --json`) via `topics.LoadTrends`.
Per-site pages filter each topic's items to that site's feeds and rewrite the
score to the filtered count.

## Desired end state (locked in the mockup)

- **Grouped by status**, in the order new → growing → fading → steady → quiet.
  Quiet is shown as the last group, not collapsed. Empty groups are omitted.
- **Header box pills** are grouped the same way. Each group starts with a
  standalone header line — a status-coloured dot, the status name and its
  count — above that group's pills. Each pill also carries a status dot.
- **Cards** are grouped under a heading per status (coloured status badge +
  "N topics"). Each card summary shows: status badge, label, the existing
  "N items" badge, then on the right a **stats strip** — `F feeds`,
  `T today ▲d / ▼d / ·` (last 24h and its change vs the prior 24h), and
  `+N since last run` (omitted for `new` topics and when N is 0) — followed by
  an **SVG sparkline** and the `↑ Top` link.
- **Sort** within each group: last 24h descending, then item count descending.
- **Growing/fading threshold: 1** — exactly slice 2's rule, no new threshold.
- **Meta line** gains the status tally, e.g. "… — 16 new · 4 growing ·
  14 fading · 1 steady · 36 quiet".
- **Permalinks**: pills link to and cards carry `id="thread-{ThreadID}"`, stable
  across runs. `topic-navigator.js` opens cards for `#thread-` links and hashes.

## Design decisions

- **Decision:** trends are computed at render time with `topics.LoadTrends`; no
  new storage.
  - **Why:** slice 2 built exactly this path. Renderer → topics adds no import
    cycle (`research.md` Dependencies).

- **Decision:** on per-site pages, trends are computed over **that site's
  items only**, for both the current and the previous topic.
  - **Why:** the card already shows the site-filtered item count; a sparkline
    or "+3 since last run" counting other sites' items would contradict it.
    Filtering only the current side would count other sites' items as
    "dropped", so the previous members are filtered too.
  - **How:** `topics.LoadTrendsForFeeds(ctx, db, run, topics, topicItems,
    allowedFeeds map[string]bool)`; `nil` means no filter, and `LoadTrends`
    becomes a call to it with `nil`. Previous members whose item row is gone
    (purged) are ignored when a filter is set, since their feed is unknowable.
  - **Rejected:** global trends on every page — simpler, but numbers disagree
    with what the page lists.

- **Decision:** status order lives in `internal/trends` as
  `trends.StatusOrder []string` (a function, to satisfy gochecknoglobals).
  - **Why:** renderer grouping and any future consumer agree on one order.

- **Decision:** grouping, sorting and the tally are computed in Go into the
  template context; the template only iterates.
  - **Why:** testable in `internal/renderer`; Go templates are poor at
    sort/group. New context fields: `Trends map[int64]trends.Trend` (by topic
    ID), `StatusGroups []TopicStatusGroup{Status string; Topics []*database.Topic}`,
    `StatusTally string`.

- **Decision:** the SVG sparkline is produced by a template func `sparkline`
  (`[]int → template.HTML`) in `internal/renderer/templates.go`.
  - **Why:** same place as the existing funcs; values are integers so the
    markup is built safely with `fmt`, not from strings. Bars 6px wide, 2px gap,
    18px tall, scaled to the topic's own peak, minimum 1px; the last bar uses
    the primary text colour, the rest tertiary; a `<title>` and `aria-label`
    carry the raw counts.

- **Decision:** status colours are CSS custom properties in `variables.css`
  (`--st-new` purple, `--st-growing` green, `--st-fading` orange, `--st-steady`
  grey, `--st-quiet` light grey, darker quiet in dark mode), copied from the
  mockup.

- **Decision:** anchors switch from `#topic-{ID}` to `#thread-{ThreadID}`
  outright, no alias.
  - **Why:** old `#topic-` IDs change every run, so no existing link could
    have kept working anyway.

## Patterns to follow

- Context building and filtering: `BuildTopicsContext` / `filterSingleTopicItems`
  (`internal/renderer/workflow.go:396-492`).
- Template funcs: `internal/renderer/templates.go:66-90`.
- Topic page tests: `workflow_test.go:520-688`, `sitegroup/render_test.go:541`.
- Pure helpers + table tests: `internal/trends/trends_test.go`.
- CSS: extend `feed.css` topic section; variables with dark-mode block in
  `variables.css`.
- Visual values (colours, sizes, spacing): port from
  `tmp/topic-render-mockup/index.html`'s candidate-design CSS block.

## What we're NOT doing

- No new thresholds or ranking scores; no user-facing sort/filter controls on
  the page.
- No thread history page or per-thread URLs beyond the in-page anchor.
- No changes to `index.html`, the feed pages, clustering, lineage, or trends
  arithmetic beyond `StatusOrder` and the feed filter.
- No JavaScript-side rendering: the page stays static HTML; the navigator only
  opens/closes cards.
- No `internal/api` exposure.

## Open questions

- **Topics without a thread (ThreadID 0)?** Default: can't occur after
  migration 14's backfill; if one does, it renders with `id="topic-{ID}"` and
  no permalink. Covered by a test.
- **Per-site status counts in the meta line?** Default: yes, per site — the
  tally is computed from that page's trends.

## Revisions during review (2026-09-22)

After seeing the rendered page, Les asked for:
- **Growth margin 2, configurable**: growing/fading need a change of at least
  `topics.growth_margin` (default 2) items in the last 24h vs the 24h before;
  `topics latest --growth-margin` and `render --topic-growth-margin` override
  it. Quiet became explicitly "zero in both windows". This supersedes
  "Growing/fading threshold: 1" above.
- **Card-header status as a coloured dot at every width** (the group heading
  names it); the change arrow is coloured by status.
- **Narrow screens**: one-line card headers with ellipsis, count badge without
  "items", options trigger as just ⚙.
- **Header nav link reads "Trending"** on every page.
