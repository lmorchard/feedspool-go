# Dev Session Spec: Site Title From Feed List

## Overview

Every generated page currently hardcodes the string `feedspool` as its `<h1>`,
and the per-site `index.html` also hardcodes it as its `<title>`. In directory
mode this is actively wrong: a build of five feed lists produces five sites
that are indistinguishable from each other in a browser tab, a bookmark, or a
history entry, even though the top-level index already labels each one
correctly.

This spec threads the feed list's own title down to the pages rendered from
that list. An OPML's `<head><title>` becomes the site's title; a text list or
an untitled OPML falls back to the filename base; a render with no feed list
at all keeps saying `feedspool`.

The data already exists. `sitegroup.Site.Title` is populated during discovery
and drives the top-level index links, and `renderer.loadFeedURLs` already
parses the feed list and holds `feedList.Title()` in hand before discarding it.
The gap is plumbing: `renderer.WorkflowConfig` has no title field, so the value
dies at the boundary between `sitegroup` and `renderer`.

## Goals

1. Per-site `index.html` uses the feed list title for both `<title>` and `<h1>`.
2. Per-feed pages (`feeds/<id>.html`) use it for `<h1>` and as the `<title>`
   suffix, replacing the generic `Feed Reader`.
3. Single-list mode gets the same behavior as directory mode, from the same
   field — not a parallel code path.
4. Database mode (no feed list) is unchanged: `feedspool` everywhere.
5. Custom templates extracted via `init --extract-templates` keep rendering
   without error. They will still show their own hardcoded `feedspool` until
   re-extracted; the requirement is that no template reference breaks.

## Non-Goals

- The top-level `site-index.html`. It spans every list and has no single OPML
  to draw from; it keeps its hardcoded `feedspool`.
- A config key for overriding the title (`render.site_title` or similar).
- `feed-list-page.html`. It is a pagination fragment with no `<title>` or
  `<h1>`.
- Any change to how `sitegroup` derives `Site.Title`, or to the top-level index
  link labels, which are already correct.

## Behavior

| Invocation | `index.html` `<title>`/`<h1>` | `feeds/<id>.html` |
|---|---|---|
| `render --feeds-dir ./feeds.d` | per-site OPML head title, else filename base | `<title>Warp Records - disquiet</title>`, `<h1>disquiet</h1>` |
| `render --feeds list.opml` | that OPML's head title | `<title>Warp Records - Feeds exported from Linkding</title>` |
| `render --feeds list.txt` | filename base (`list`) | `<title>Warp Records - list</title>` |
| `render` (database mode) | `feedspool` | `<title>Warp Records - feedspool</title>`, `<h1>feedspool</h1>` |
| top-level `build/index.html` | `feedspool` (unchanged) | n/a |

The fallback chain is: explicit override from the caller, then the feed list's
own title, then the feed list's filename base, then the literal `feedspool`
baked into the template.

## Architecture

### `SiteChrome`

Introduce a struct in `internal/renderer` holding the page furniture every
template needs:

```go
// SiteChrome is the page furniture every template needs: what the site is
// called, what time window it covers, and when it was built.
type SiteChrome struct {
	SiteTitle   string
	TimeWindow  string
	GeneratedAt time.Time
}
```

Embed it in `TemplateContext`, `FeedTemplateContext`, and `PageTemplateContext`,
replacing the `TimeWindow` and `GeneratedAt` fields those three structs each
declare today.

Go's `html/template` resolves promoted fields of embedded structs, so existing
`{{.TimeWindow}}` and `{{.GeneratedAt}}` references in both embedded and
user-extracted templates keep working with no edit. Promotion also survives the
existing second level of embedding: `renderIndexFile` wraps the context in a
local `IndexContext` that embeds `*TemplateContext`.

This is chosen over adding a bare `siteTitle string` parameter because the
affected render helpers already take 8 to 10 positional arguments. Collapsing
three fields into one embedded struct makes those signatures shorter than they
are today rather than longer:

| Function | Today | With `SiteChrome` | With a bare `siteTitle` param |
|---|---|---|---|
| `generateSite` | 5 | 4 | 6 |
| `createTemplateContext` | 7 | 5 | 8 |
| `renderFeedPages` | 10 | 9 | 11 |
| `renderIndividualFeeds` | 8 | 7 | 9 |
| `renderSingleFeed` | 8 | 7 | 9 |

`generateSite` and `createTemplateContext` shrink most because `SiteChrome`
absorbs `startTime`, `endTime`, and `maxAge`: `FormatTimeWindow` is called once
in `ExecuteWorkflow` to build the chrome, rather than three times inside
`generateSite` as it is today.

The three values always travel together and are the only fields all three
context structs share, so the grouping is real rather than a bag of convenience.

### `WorkflowConfig.SiteTitle`

Add a `SiteTitle string` field to `renderer.WorkflowConfig`. It is an
*override*, not the sole source:

- Empty: `ExecuteWorkflow` derives the title from the loaded feed list.
- Non-empty: the caller already knows the title and it is used verbatim.

`sitegroup.renderOneSite` sets it from `Site.Title`, alongside the `OutputDir`,
`FeedsFile`, and `Format` it already overrides on its per-site config copy.
Database mode sets neither the override nor a feed list, so the value stays
empty and the template's literal default shows through with no conditional
anywhere in the render path.

### `loadFeedURLs`

Change the signature from `(feedsFile, format string) ([]string, error)` to
also return the derived title. It already calls `feedlist.LoadFeedList`, so
`feedList.Title()` costs nothing extra — there is no second parse.

Apply the filename-base fallback here, so a `.txt` list and an OPML with no
`<head><title>` both produce a usable name rather than an empty string. The
fallback is `strings.TrimSuffix(filepath.Base(feedsFile), filepath.Ext(feedsFile))`
— the same derivation `sitegroup.Discover` already applies, so a given file
yields the same title in both modes.

Return an empty title when `feedsFile` is empty, preserving the existing
early return for database mode.

## Templates

`internal/renderer/templates/index.html`:

```
<title>feedspool</title>     ->  <title>{{.SiteTitle}}</title>
<h1>feedspool</h1>           ->  <h1>{{.SiteTitle}}</h1>
```

`internal/renderer/templates/feed.html`:

```
<title>{{.Feed.Title}} - Feed Reader</title>  ->  <title>{{.Feed.Title}} - {{.SiteTitle}}</title>
<h1>feedspool</h1>                            ->  <h1>{{.SiteTitle}}</h1>
```

`site-index.html` is untouched.

Both templates also carry a footer reading `Generated by feedspool at ...`
(`index.html:76`, `feed.html:112`). That `feedspool` names the tool that built
the page rather than the site, and is deliberately left alone. Only the
`<title>` and `<h1>` occurrences change.

Because the fallback chain terminates in `ExecuteWorkflow` rather than in the
template, `SiteTitle` is never empty by the time a template renders it. The
literal `feedspool` default moves from the template into the workflow.

## Edge Cases

- **Untitled OPML** — `<head>` present but no `<title>`, or no `<head>` at all.
  Falls back to the filename base.
- **Whitespace-only OPML title** — treated as absent; falls back to the
  filename base rather than rendering a blank header.
- **Text list** — `TextFeedList.Title()` returns empty by construction. Falls
  back to the filename base, matching directory-mode behavior for `.txt`.
- **HTML metacharacters in a title** — `html/template` escapes contextually in
  both `<title>` and `<h1>`; no manual escaping is added.
- **Directory mode with a skipped list** — a list that fails to parse is
  already skipped before any render, so no site is produced and no title is
  needed.
- **Custom extracted templates** — a user who extracted templates before this
  change has a copy with the hardcoded `feedspool`. Their output is unchanged
  until they re-extract or hand-edit. This is expected and not worked around.

## Testing

### `internal/renderer`

- `loadFeedURLs` returns the OPML `<head><title>`.
- `loadFeedURLs` falls back to the filename base for an OPML with no title,
  for a whitespace-only title, and for a `.txt` list.
- `loadFeedURLs` returns an empty title when no feed list is given.
- `ExecuteWorkflow` with an OPML feed list writes that title into both
  `index.html` and `feeds/<id>.html`.
- `ExecuteWorkflow` with `SiteTitle` set uses the override in preference to the
  feed list's own title.
- `ExecuteWorkflow` in database mode still writes `feedspool`.
- A feed list whose title contains HTML metacharacters renders escaped.
- A render through a custom templates directory still resolves `{{.TimeWindow}}`
  and `{{.GeneratedAt}}`, which after the embed reach the template only by
  field promotion. This is the only coverage of Goal 5; nothing in the suite
  exercises a custom template directory today.

### `internal/sitegroup`

- Two feed lists in one directory render *different* titles into their
  respective subdirectories — the case that catches a config-copy bug in
  `renderOneSite`.
- The top-level index link labels are unchanged, which is the regression signal
  that `site-index.html` was left alone.

### Verification

`make format`, `make lint`, `make test` all clean. Then a manual directory-mode
build confirming two sites carry two distinct titles.

## Documentation

`MANUAL.md` gains a short paragraph in the Multi-Site Directory Mode "Naming"
section: the OPML head title now sets each site's page title and header, not
just its label on the index. The existing description of slug-versus-label
derivation is already accurate and needs no change.
