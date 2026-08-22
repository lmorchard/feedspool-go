# Dev Session Spec: Multi-OPML Site Groups

## Overview

Today `feedspool` fetches one feed list and renders one site. This spec adds a
directory mode: point `fetch` and `render` at a directory of OPML and text feed
lists, and feedspool builds one site per list plus a top-level index page
linking them all.

The work splits into two phases that mirror the existing `fetch`/`render` split:

1. **Fetch phase** — discover every feed list in the directory, union and dedupe
   their feed URLs, and run a single fetch pass. A feed referenced by five lists
   is fetched once.
2. **Render phase** — render one site per feed list into `<output>/<slug>/`,
   filtered from the shared database, then render a top-level index page.

Motivating use cases: topical collections (separate "newspapers" for tech, news,
comics) and curated public pages (shareable per-topic aggregators behind a
directory index).

## Goals

1. Build N sites from a directory of feed lists with one deduped fetch pass.
2. Generate a top-level index page showing each site's title, feed count, item
   count, and freshness.
3. Keep each generated site self-contained and independently deployable.
4. Leave all existing single-list behavior byte-for-byte unchanged.
5. Survive partial failure: one malformed list or failed render must not sink a
   scheduled publish.

## Non-Goals

- Recursive directory scanning / nested site hierarchies.
- Sharing one asset bundle across sites.
- Parallel site rendering.
- An aggregate "river" view spanning all collections.
- Per-site render overrides (custom max-age, templates) via a manifest file.

## Output Structure

```
build/
├── index.html               # site directory index
├── site-index.css
├── site-index.js
├── .feedspool-sites.json    # prune manifest
├── tech-blogs/
│   ├── index.html
│   ├── index.css
│   ├── index.js
│   └── feeds/…
├── comics/
│   └── …
└── scratch/
    └── …
```

Every site subdirectory is byte-identical to what a single-list
`render --output <dir>` produces today, including its own copy of the CSS/JS
bundle. Assets are referenced with bare relative paths (`href="index.css"`), so
any subdirectory can be deployed on its own.

## CLI Surface

```
feedspool fetch  --feeds-dir ./opml/    # discover → union → dedupe → one fetch pass
feedspool render --feeds-dir ./opml/    # render each site → prune → render index
feedspool build  --feeds-dir ./opml/    # convenience: fetch then render
```

### Flag naming

The flag is `--feeds-dir`, not `--dir`, because `serve --dir` already means "the
directory to serve". Config key: `feedlist.dir`.

### `build` command scope

`build` exposes only `--feeds-dir`, `--output`, `--clean`, and `--with-unfurl`.
Everything else comes from the config file.

`build` also works in single-list mode: with no `--feeds-dir` and no
`feedlist.dir` in config, it runs the ordinary single-list fetch then render,
using `feedlist.filename` / `feedlist.format`. It is a fetch-then-render
convenience in both modes, not a directory-only command.

This is deliberate. `fetch --max-age` means "skip feeds fetched within this
duration" and `render --max-age` means "the display time window" — the same flag
name with opposite meanings. Rather than invent `--fetch-max-age` /
`--render-max-age`, `build` stays a thin cron convenience and defers all tuning
to config.

### Mutual exclusion

- `--feeds-dir` together with `--filename` or `--format` is an error.
- Config containing both `feedlist.dir` and `feedlist.filename` is an
  ambiguous-config error at load time.
- An explicit `--filename` flag overrides a configured `feedlist.dir`
  (single-file mode wins when asked for directly).
- With neither `--feeds-dir` nor `feedlist.dir`, every existing code path
  behaves exactly as it does today.

## Architecture

### New package: `internal/sitegroup`

Owns discovery, URL union, prune, and index-render orchestration.

```go
type Site struct {
    Slug   string          // slugified filename base
    Title  string          // OPML <head><title>, else filename base verbatim
    Path   string          // path to the .opml/.txt file
    Format feedlist.Format // inferred from extension
    URLs   []string
}

type SiteResult struct {
    Site
    FeedCount  int
    ItemCount  int
    NewestItem time.Time // zero = no items in window
    Err        error     // non-nil if this site failed to render
}

// Skipped records a file that could not be loaded. Discover returns these
// alongside the sites that did load, rather than failing the whole run.
type Skipped struct {
    Path string
    Err  error
}

func Discover(dir string) (sites []Site, skipped []Skipped, err error)
func UnionURLs(sites []Site) []string
func Prune(outputDir string, discovered []Site) (removed []string, err error)
func WriteManifest(outputDir string, discovered []Site) error
```

`Discover` returns a non-nil `err` only for whole-directory failures (missing
directory, no feed lists found, slug collision, unsafe slug). A file that fails
to parse lands in `skipped` and does not stop the run; callers log each entry
and set a non-zero exit status.

`cmd/` stays thin — flag parsing and config merging only, per CLAUDE.md's
convention that `cmd/` is untested and `internal/` is tested.

### Changes to existing packages

| Package | Change | Rationale |
|---|---|---|
| `internal/feedlist` | Add `Title() string` to the `FeedList` interface | OPML title is parsed in `internal/opml/parser.go` but discarded by `GetURLs()`. `TextFeedList.Title()` returns `""`. |
| `internal/fetcher` | Extract `FetchFromURLs(ctx, urls, opts) []*FetchResult`; `FetchFromFile` becomes a wrapper around it | Union fetch without duplicating concurrency, unfurl, or remove-missing logic |
| `internal/renderer` | `ExecuteWorkflow` returns `(*Result, error)` instead of `error` | The index page needs feed count, item count, and newest-item time. Single-list callers ignore the result. |

```go
// renderer.Result summarizes what a single ExecuteWorkflow call produced.
type Result struct {
    FeedCount  int       // feeds matching the window and feed-list filter
    ItemCount  int       // items rendered across those feeds, after
                         // MinItemsPerFeed / MaxItemsPerFeed are applied
    NewestItem time.Time // newest PublishedDate across rendered items;
                         // zero when no items were rendered
}
```

`sitegroup` copies these three fields straight into `SiteEntry`, so the index
page reports exactly what each site actually rendered — not what the database
holds.
| `internal/renderer` | New `RenderSiteIndex(outputDir string, ctx *SiteIndexContext) error` + `site-index.html` template | Index rendering belongs with the other templates so `init --extract-templates` picks it up automatically |

`FetchFromFile` today is `load URLs → fetchConcurrentWithUnfurl(urls) →
removeMissingFeeds(urls)`. Extracting `FetchFromURLs` is a mechanical split.

## Discovery

`sitegroup.Discover(dir)`:

1. Non-recursive `os.ReadDir(dir)`.
2. Keep regular files with a `.opml` or `.txt` extension, case-insensitive.
   Skip subdirectories and all other files silently.
3. Sort by filename for stable, predictable output ordering.
4. For each file: infer format from extension, load via
   `feedlist.LoadFeedList`, take `URLs` from `GetURLs()` and `Title` from
   `Title()` with the filename base as fallback.
5. Slug = slugified filename base: lowercase, runs of non-alphanumeric
   characters collapsed to `-`, leading/trailing `-` trimmed.

Errors:

- Directory missing or not a directory → hard error.
- No `.opml`/`.txt` files found → hard error naming the directory.
- Two files producing the same slug → hard error naming both files.
- A slug that resolves to empty, `.`, or `..` → hard error naming the file.
- A single file that fails to parse → warn, skip that site, continue. The
  process exits non-zero at the end.

## Fetch Phase

```
Discover(dir) → UnionURLs(sites) → orchestrator.FetchFromURLs(ctx, union, opts)
```

As in the render phase, each `Skipped` entry is logged and sets the non-zero
exit status without stopping the fetch.

`UnionURLs` dedupes while preserving first-seen order across sites (sites
themselves being in filename order), so fetch ordering is deterministic.

Logs the dedup win explicitly, e.g.
`Found 4 feed lists, 87 unique feeds (103 references)`.

`--remove-missing` receives the union. This is the only correct semantics —
running remove-missing per file in a loop would have each list delete the other
lists' feeds. The phase split makes this correct by construction.

Results feed the existing `FetchSummary` with `Mode = "dir"`.

## Render Phase

1. If `--clean`, remove the entire output directory once, up front.
2. `Discover(dir)`. Log each `Skipped` entry; a non-empty `skipped` slice sets
   the non-zero exit status but does not stop the run.
3. For each site, sequentially: call `renderer.ExecuteWorkflow` with
   `OutputDir = <output>/<slug>`, `FeedsFile = site.Path`,
   `Format = site.Format`, and `Clean: false`. Collect a `SiteResult`.
   - On error: warn, record `Err`, continue with remaining sites.
4. `Prune(outputDir, discovered)`.
5. `WriteManifest(outputDir, discovered)`.
6. Render the index page and copy its assets.

Rendering is sequential. Each `ExecuteWorkflow` opens its own SQLite connection
and the work is write-I/O bound, so parallelism buys little and complicates
progress output. Revisit only if site counts reach the hundreds.

### Index inclusion rule

**A site is linked on the index only if `<output>/<slug>/index.html` exists on
disk after the render pass.**

One `os.Stat` per site, and it resolves both awkward cases correctly:

- Failed this run but built successfully before → stays linked, serving slightly
  stale content. Better than a dead link.
- Never built → omitted from the index entirely.

### Pruning

**`prune = slugs in previous manifest − slugs discovered this run`.**

Keying off *discovered* rather than *successfully rendered* slugs is essential:
a site that failed to render is still discovered, so its directory is never
deleted out from under it.

Manifest at `<output>/.feedspool-sites.json`:

```json
{"version": 1, "slugs": ["comics", "news", "tech-blogs"]}
```

An absent manifest (first run, or after `--clean`) means nothing to prune.

Prune removes a path only if all of the following hold:

1. It is listed in the previous manifest.
2. It resolves to a direct child of the output root (never the root itself, no
   path traversal).
3. It exists and is a directory.

Prune can therefore never touch a directory feedspool did not create.

## Index Page

### Template

`internal/renderer/templates/site-index.html`, rendered by
`renderer.RenderSiteIndex`. Living alongside `index.html` and `feed.html` means
`feedspool init --extract-templates` extracts it automatically and users
customize it identically — no second extraction path.

### Context

```go
type SiteIndexContext struct {
    Sites       []SiteEntry
    GeneratedAt time.Time
    TimeWindow  string // same string the per-site pages display
}

type SiteEntry struct {
    Slug       string
    Title      string
    FeedCount  int
    ItemCount  int
    NewestItem time.Time // zero = no items in window
}
```

Sites render in filename order. No sort-by-freshness: a directory whose entries
reshuffle on every render is harder to navigate by muscle memory, and file
naming already gives explicit control over ordering.

### Markup

```html
<time-formatter>
  <ul class="site-list">
    {{range .Sites}}
    <li class="site-entry{{if eq .ItemCount 0}} site-entry-quiet{{end}}">
      <a class="site-link" href="{{.Slug}}/">{{.Title}}</a>
      <p class="site-stats">
        {{.FeedCount}} feeds · {{.ItemCount}} new items
        {{if not .NewestItem.IsZero}}
        · newest <time datetime="{{.NewestItem.Format "2006-01-02T15:04:05Z07:00"}}">{{.NewestItem.Format "Jan 2, 2006 15:04 UTC"}}</time>
        {{end}}
      </p>
    </li>
    {{end}}
  </ul>
</time-formatter>
```

Timestamps are emitted as absolute ISO inside `<time datetime>` and upgraded to
relative time client-side by the existing `<time-formatter>` custom element.
Baking "22m ago" into static HTML at build time would be wrong a minute later.

### Assets

The index gets its own thin bundle rather than reusing the feed reader's:

- `internal/renderer/assets/site-index.css` — `@import` of `css/variables.css`,
  `css/base.css`, and a new `css/site-index.css` holding the site-list rules.
  Inherits the existing design tokens and dark mode without pulling in
  feed/item/lightbox styles.
- `internal/renderer/assets/site-index.js` — imports and registers
  `TimeFormatter` only.

Both are copied into the output root. Per-site directories keep their existing
full `index.css`/`index.js` bundles, unchanged.

## Behavior Changes to Existing Code

Two intentional changes to current single-list behavior:

1. **`ExecuteWorkflow` always writes `index.html`, even with zero matching
   feeds.** Today it returns early and writes nothing, which in directory mode
   would produce a dead index link. It also fixes a current single-list oddity:
   a no-match render silently leaves the previous build in place.

2. **`ExecuteWorkflow` returns `(*Result, error)`.** All existing callers ignore
   the new return value; no behavior change for them.

## Edge Cases

| Case | Behavior |
|---|---|
| Feed listed in three OPMLs | Fetched once; appears in all three sites. Free — render filters the shared DB by URL list. |
| OPML with zero feeds | Still a site. Empty page, `0 feeds · 0 new items` on the index. |
| Nested OPML outlines | Already recursed by `opml.ExtractFeedURLs`. No change. |
| `Tech Blogs.opml` | Slug `tech-blogs`, title from `<head><title>` or `Tech Blogs`. |
| `scratch.txt` | Slug `scratch`, title `scratch` (text lists carry no title). |
| `README.md` in the directory | Ignored silently. |
| Subdirectory in the directory | Ignored silently. |

## Failure Policy

One bad file must not sink a scheduled publish.

| Failure | Behavior | Exit |
|---|---|---|
| `--feeds-dir` missing or is a file | Hard error before any work | non-zero |
| No feed lists found in directory | Hard error naming the directory | non-zero |
| Slug collision between two files | Hard error naming both files | non-zero |
| One feed list fails to parse | Warn, skip that site, continue | non-zero |
| Individual feed fetch fails | Existing per-feed handling; never aborts | zero |
| One site fails to render | Warn, continue remaining sites | non-zero |

Non-zero exit on partial failure matters for cron and CI: the site still
publishes, but the run is visibly not clean.

## Docker

`docker-entrypoint.sh` currently auto-detects `feeds.txt` / `feeds.opml`. Add
detection for a `/data/feeds.d/` directory, checked *before* those two, so the
container gets multi-site mode with no configuration.

## Testing

Per CLAUDE.md, `internal/` is tested and `cmd/` is not.

### `internal/sitegroup`

- Discovery over a fixture directory: mixed extensions, ignored files, ignored
  subdirectories, filename ordering.
- Title extraction: OPML with `<head><title>`, OPML without, text list fallback.
- Slugification: spaces, uppercase, punctuation runs.
- Errors: slug collision, empty directory, missing directory, path-unsafe slug.
- Malformed OPML: returned in `skipped`, with the remaining sites intact and
  `err` nil.
- `UnionURLs`: dedupes across overlapping lists, preserves first-seen order.
- `Prune`: removes a departed slug; leaves directories absent from the manifest
  untouched; no-ops when the manifest is missing; never removes the output root.
- `WriteManifest` / read round-trip.

### `internal/feedlist`

- `Title()` for OPML (with and without a head title) and for text lists.

### `internal/fetcher`

- Direct `FetchFromURLs` test.
- Existing `FetchFromFile` tests must pass unchanged.

### `internal/renderer`

- `ExecuteWorkflow` returns populated feed count, item count, and newest-item
  time.
- Zero-matching-feeds case writes `index.html`.
- `RenderSiteIndex` emits correct links, counts, `datetime` attributes, and the
  `site-entry-quiet` class on zero-item entries.

### `integration_test.go`

End-to-end against an `httptest` feed server: a temp directory with two OPMLs
sharing one feed. Run `fetch --feeds-dir` then `render --feeds-dir`. Assert:

- The shared feed was requested exactly once.
- Both site directories built with their own asset copies.
- The index links both sites with correct feed and item counts.
- Removing one OPML and re-rendering prunes its directory and drops it from the
  index.

### Verification

`make format`, `make lint`, `make test` after each set of changes.

## Documentation

- MANUAL.md: a multi-site section covering `--feeds-dir` on fetch/render, the
  `build` command, output layout, the prune manifest, and the failure policy.
- README.md: a line in the quick start.
