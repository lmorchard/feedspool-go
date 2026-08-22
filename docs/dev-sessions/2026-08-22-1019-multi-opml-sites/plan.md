# Multi-OPML Site Groups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Point `feedspool` at a directory of OPML/text feed lists and build one site per list — from a single deduped fetch pass — plus a top-level index page linking them all.

**Architecture:** A new `internal/sitegroup` package discovers feed lists in a directory, unions their URLs for one fetch pass, drives one `renderer.ExecuteWorkflow` call per list into `<output>/<slug>/`, prunes directories for lists that were deleted, and renders a top-level index. Three existing packages get small backward-compatible widenings: `feedlist` gains `Title()`, `fetcher` gains `FetchFromURLs`, and `renderer.ExecuteWorkflow` starts returning per-site stats.

**Tech Stack:** Go 1.21+, Cobra (CLI), Viper (config), SQLite (`internal/database`), `html/template` + `embed` (rendering). Tests are stdlib `testing`, same-package (`package foo`, not `foo_test`).

**Spec:** `docs/dev-sessions/2026-08-22-1019-multi-opml-sites/spec.md`

## Global Constraints

- **Build with `make build`, never `go build`.** The Makefile injects version ldflags and names the binary `feedspool`.
- **After every task: `make format`, then `make lint`, then `make test`.** All three must pass before committing. The baseline is clean — `make lint` reports **0 issues** before this plan starts, so any finding it reports is yours and must be fixed, never suppressed.
- **`internal/` is tested; `cmd/` is not.** Put logic in `internal/` so it can be tested; keep `cmd/` to flag parsing and config merging.
- **Lint rules that will bite you** (from `.golangci.yml`):
  - `lll`: max line length **120**.
  - `funlen`: max **100 lines / 50 statements** per function.
  - `cyclop`: max complexity **15**, package average **10.0**.
  - `godot`: every comment must end with a period.
  - `goconst`: a string literal repeated **2+** times must become a constant. This applies to test files too.
  - `mnd`: magic numbers must be named constants. Allowed bare: `0,1,2,8,10,16,32,60,64,100,256,404,500,503,1024`.
  - `forbidigo`: `fmt.Print*` is banned in `internal/`. **Use `logrus` for all user-facing output in `internal/sitegroup`.** Existing `internal/renderer/workflow.go` uses `//nolint:forbidigo` comments; do not add new ones.
  - `gochecknoglobals`: banned outside `cmd/`, except error sentinels named with an `Err`/`err` prefix.
  - `prealloc`: preallocate slices whose length is known.
- **Test package convention:** tests live in the same package as the code (`package sitegroup`, not `package sitegroup_test`). `testpackage` is disabled for `_test.go`.
- **Commit after each task** with a `feat:`/`refactor:`/`test:`/`docs:` prefix.

---

## File Structure

### New files

| Path | Responsibility |
|---|---|
| `internal/sitegroup/site.go` | `Site`, `Skipped`, `slugify`, `Discover`, `UnionURLs` — turning a directory into a list of sites |
| `internal/sitegroup/site_test.go` | Tests for the above |
| `internal/sitegroup/manifest.go` | `Manifest`, `ReadManifest`, `WriteManifest`, `Prune` — stale-directory tracking |
| `internal/sitegroup/manifest_test.go` | Tests for the above |
| `internal/sitegroup/render.go` | `SiteResult`, `RenderSummary`, `RenderAll`, `PlanFetch`, `ErrPartialFailure` — orchestration |
| `internal/sitegroup/render_test.go` | Tests for the above |
| `internal/renderer/siteindex.go` | `SiteIndexContext`, `SiteEntry`, `RenderSiteIndex` |
| `internal/renderer/siteindex_test.go` | Tests for the above |
| `internal/renderer/templates/site-index.html` | The index page template |
| `internal/renderer/assets/site-index.css` | `@import` bundle for the index page |
| `internal/renderer/assets/css/site-index.css` | Site-list rules |
| `internal/renderer/assets/site-index.js` | Registers `<time-formatter>` only |
| `cmd/build.go` | The `build` convenience command |

### Modified files

| Path | Change |
|---|---|
| `internal/feedlist/feedlist.go` | Add `Title()` to the `FeedList` interface and both implementations |
| `internal/feedlist/feedlist_test.go` | Tests for `Title()` |
| `internal/fetcher/orchestrator.go` | Extract `FetchFromURLs`; `FetchFromFile` delegates to it |
| `internal/fetcher/orchestrator_test.go` | New file — tests for `FetchFromURLs` |
| `internal/renderer/workflow.go` | `ExecuteWorkflow` returns `(*Result, error)`; always writes `index.html` |
| `internal/renderer/workflow_test.go` | New file — tests for `Result` and the zero-feed case |
| `internal/config/config.go` | Add `FeedList.Dir`; add `HasFeedListDir()` |
| `internal/config/config_test.go` | Tests for the above |
| `cmd/fetch.go` | Add `--feeds-dir`; directory-mode branch |
| `cmd/render.go` | Add `--feeds-dir`; directory-mode branch |
| `cmd/root.go` | Register `buildCmd` (via `cmd/build.go`'s `init`) |
| `.golangci.yml` | Add `build` to the two `cmd/(...)` exclusion regexes |
| `integration_test.go` | End-to-end directory-mode test |
| `feedspool.yaml.example` | Document `feedlist.dir` |
| `MANUAL.md` | Multi-site section |
| `README.md` | Quick-start line |
| `docker-entrypoint.sh` | Detect `/data/feeds.d/` |

---

## Task 1: `feedlist.Title()`

The OPML `<head><title>` is parsed into `opml.Head.Title` but thrown away — `FeedList` only exposes URLs. The index page needs it for link text.

**Files:**
- Modify: `internal/feedlist/feedlist.go` (interface at ~line 32; `OPMLFeedList` methods at ~line 128; `TextFeedList` methods at ~line 217)
- Test: `internal/feedlist/feedlist_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `feedlist.FeedList` interface gains `Title() string`. Returns the OPML head title with surrounding whitespace trimmed, or `""` for text lists and untitled OPML.

- [ ] **Step 1: Write the failing tests**

Append to `internal/feedlist/feedlist_test.go`:

```go
func TestOPMLFeedListTitle(t *testing.T) {
	const withTitle = `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
    <head><title>  Tech Blogs  </title></head>
    <body><outline text="A" type="rss" xmlUrl="https://example.com/feed.xml" /></body>
</opml>`

	list, err := loadOPMLFeedList(strings.NewReader(withTitle))
	if err != nil {
		t.Fatalf("loadOPMLFeedList() error = %v", err)
	}
	if got := list.Title(); got != "Tech Blogs" {
		t.Errorf("Title() = %q, want %q", got, "Tech Blogs")
	}
}

func TestOPMLFeedListTitleAbsent(t *testing.T) {
	const noTitle = `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
    <head></head>
    <body><outline text="A" type="rss" xmlUrl="https://example.com/feed.xml" /></body>
</opml>`

	list, err := loadOPMLFeedList(strings.NewReader(noTitle))
	if err != nil {
		t.Fatalf("loadOPMLFeedList() error = %v", err)
	}
	if got := list.Title(); got != "" {
		t.Errorf("Title() = %q, want empty string", got)
	}
}

func TestTextFeedListTitle(t *testing.T) {
	list, err := loadTextFeedList(strings.NewReader(testURL1 + "\n"))
	if err != nil {
		t.Fatalf("loadTextFeedList() error = %v", err)
	}
	if got := list.Title(); got != "" {
		t.Errorf("Title() = %q, want empty string", got)
	}
}
```

`strings` and `testURL1` are already imported/defined in that file.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/feedlist/ -run 'Title' -v`
Expected: compile failure — `list.Title undefined (type FeedList has no field or method Title)`.

- [ ] **Step 3: Add `Title()` to the interface**

In `internal/feedlist/feedlist.go`, change the `FeedList` interface:

```go
// FeedList interface provides unified access to different feed list formats.
type FeedList interface {
	GetURLs() []string
	Title() string
	AddURL(url string) error
	RemoveURL(url string) error
	Save(filename string) error
}
```

- [ ] **Step 4: Implement both methods**

Add next to `func (ofl *OPMLFeedList) GetURLs()`:

```go
// Title returns the OPML head title, or an empty string if it is unset.
func (ofl *OPMLFeedList) Title() string {
	return strings.TrimSpace(ofl.opml.Head.Title)
}
```

Add next to `func (tfl *TextFeedList) GetURLs()`:

```go
// Title returns an empty string; text feed lists carry no title.
func (tfl *TextFeedList) Title() string {
	return ""
}
```

`strings` is already imported in `feedlist.go`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/feedlist/ -v`
Expected: PASS, including all pre-existing tests.

- [ ] **Step 6: Verify and commit**

```bash
make format && make lint && make test
git add internal/feedlist/
git commit -m "feat: expose feed list title via FeedList.Title()"
```

---

## Task 2: `fetcher.FetchFromURLs`

Directory mode needs to fetch an arbitrary URL slice (the deduped union) with the same concurrency, unfurl, and remove-missing handling that `FetchFromFile` already has. Extract it rather than duplicate it.

**Files:**
- Modify: `internal/fetcher/orchestrator.go:58-89` (`FetchFromFile`)
- Test: `internal/fetcher/orchestrator_test.go` (create)

**Interfaces:**
- Consumes: existing `Orchestrator`, `FetchOptions`, `FetchResult`, `fetchConcurrentWithUnfurl`, `removeMissingFeeds`.
- Produces: `func (o *Orchestrator) FetchFromURLs(ctx context.Context, feedURLs []string, opts FetchOptions) []*FetchResult`. Note: **no error return** — per-feed failures are carried inside each `*FetchResult`, exactly as `FetchFromDatabase` does.

- [ ] **Step 1: Write the failing tests**

Create `internal/fetcher/orchestrator_test.go`:

```go
package fetcher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lmorchard/feedspool-go/internal/config"
)

func TestFetchFromURLs(t *testing.T) {
	db := setupTestDatabase(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(testFeedXML))
	}))
	defer server.Close()

	o := NewOrchestrator(db, config.GetDefault())
	results := o.FetchFromURLs(context.Background(), []string{server.URL}, FetchOptions{
		Timeout:     config.DefaultTimeout,
		MaxItems:    config.DefaultMaxItems,
		Concurrency: 1,
	})

	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Error != nil {
		t.Errorf("results[0].Error = %v, want nil", results[0].Error)
	}
}

func TestFetchFromURLsEmpty(t *testing.T) {
	db := setupTestDatabase(t)

	o := NewOrchestrator(db, config.GetDefault())
	results := o.FetchFromURLs(context.Background(), nil, FetchOptions{Concurrency: 1})

	if len(results) != 0 {
		t.Errorf("len(results) = %d, want 0", len(results))
	}
}
```

Before writing, confirm the `FetchResult` error field name with `grep -n 'type FetchResult' -A 15 internal/fetcher/fetcher.go` and adjust `results[0].Error` if it differs.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/fetcher/ -run FetchFromURLs -v`
Expected: compile failure — `o.FetchFromURLs undefined`.

- [ ] **Step 3: Extract `FetchFromURLs` and rewrite `FetchFromFile`**

Replace `FetchFromFile` in `internal/fetcher/orchestrator.go` with:

```go
// FetchFromURLs fetches an explicit list of feed URLs with optional unfurl.
// Per-feed failures are reported inside the returned results, not as an error.
func (o *Orchestrator) FetchFromURLs(
	ctx context.Context, feedURLs []string, opts FetchOptions,
) []*FetchResult {
	if len(feedURLs) == 0 {
		return []*FetchResult{}
	}

	results := o.fetchConcurrentWithUnfurl(ctx, feedURLs, opts)

	// Handle feed removal if requested.
	if opts.RemoveMissing {
		removedCount := o.removeMissingFeeds(feedURLs)
		if removedCount > 0 {
			logrus.Infof("Removed %d feeds not in list", removedCount)
		}
	}

	return results
}

// FetchFromFile executes fetch from a feed list file with optional unfurl.
func (o *Orchestrator) FetchFromFile(
	ctx context.Context, format feedlist.Format, filename string, opts FetchOptions,
) ([]*FetchResult, error) {
	// Load feed URLs from file.
	list, err := feedlist.LoadFeedList(format, filename)
	if err != nil {
		return nil, fmt.Errorf("failed to load feed list %s: %w", filename, err)
	}

	feedURLs := list.GetURLs()
	if len(feedURLs) == 0 {
		logrus.Infof("No feed URLs found in %s", filename)
		return []*FetchResult{}, nil
	}

	if opts.WithUnfurl {
		logrus.Infof("Found %d feeds in %s - fetching with parallel unfurl", len(feedURLs), filename)
	} else {
		logrus.Infof("Found %d feeds in %s", len(feedURLs), filename)
	}

	return o.FetchFromURLs(ctx, feedURLs, opts), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/fetcher/ -v`
Expected: PASS, including all pre-existing `FetchFromFile` tests — that delegation must be behavior-preserving.

- [ ] **Step 5: Verify and commit**

```bash
make format && make lint && make test
git add internal/fetcher/
git commit -m "refactor: extract Orchestrator.FetchFromURLs from FetchFromFile"
```

---

## Task 3: `renderer.Result` and the zero-feed case

The index page needs each site's feed count, item count, and newest item time. `ExecuteWorkflow` computes all three internally but returns only `error`. It also returns early on zero matches without writing `index.html`, which in directory mode would produce a dead index link.

**Files:**
- Modify: `internal/renderer/workflow.go:32-86` (`ExecuteWorkflow`)
- Modify: `cmd/render.go:95` (the only caller)
- Test: `internal/renderer/workflow_test.go` (create)

**Interfaces:**
- Consumes: nothing new.
- Produces:
  ```go
  type Result struct {
      FeedCount  int
      ItemCount  int
      NewestItem time.Time
  }
  func ExecuteWorkflow(config *WorkflowConfig) (*Result, error)
  ```
  `NewestItem` is the zero `time.Time` when no items were rendered. `ItemCount` counts items **after** `MinItemsPerFeed`/`MaxItemsPerFeed` are applied — it is what the site actually shows, not what the database holds.

- [ ] **Step 1: Write the failing tests**

Create `internal/renderer/workflow_test.go`:

```go
package renderer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
)

const testFeedURL = "https://example.com/feed.xml"

// newTestWorkflow builds a database with one feed and two items, and returns a
// WorkflowConfig pointing at it with a fresh output directory.
func newTestWorkflow(t *testing.T, withFeed bool) *WorkflowConfig {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := database.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InitSchema(); err != nil {
		t.Fatal(err)
	}

	if withFeed {
		now := time.Now().UTC()
		if err := db.UpsertFeed(&database.Feed{
			URL:                 testFeedURL,
			Title:               "Example",
			LastFetchTime:       now,
			LastSuccessfulFetch: now,
		}); err != nil {
			t.Fatal(err)
		}
		for i, offset := range []time.Duration{-2 * time.Hour, -1 * time.Hour} {
			if err := db.UpsertItem(&database.Item{
				FeedURL:       testFeedURL,
				GUID:          string(rune('a' + i)),
				Title:         "Item",
				Link:          "https://example.com/item",
				PublishedDate: now.Add(offset),
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	db.Close()

	return &WorkflowConfig{
		MaxAge:    "24h",
		OutputDir: filepath.Join(tmpDir, "build"),
		Database:  dbPath,
	}
}

func TestExecuteWorkflowResult(t *testing.T) {
	cfg := newTestWorkflow(t, true)

	result, err := ExecuteWorkflow(cfg)
	if err != nil {
		t.Fatalf("ExecuteWorkflow() error = %v", err)
	}
	if result.FeedCount != 1 {
		t.Errorf("FeedCount = %d, want 1", result.FeedCount)
	}
	if result.ItemCount != 2 {
		t.Errorf("ItemCount = %d, want 2", result.ItemCount)
	}
	if result.NewestItem.IsZero() {
		t.Error("NewestItem is zero, want the most recent item's published date")
	}
}

func TestExecuteWorkflowWritesIndexWhenEmpty(t *testing.T) {
	cfg := newTestWorkflow(t, false)

	result, err := ExecuteWorkflow(cfg)
	if err != nil {
		t.Fatalf("ExecuteWorkflow() error = %v", err)
	}
	if result.FeedCount != 0 {
		t.Errorf("FeedCount = %d, want 0", result.FeedCount)
	}
	if !result.NewestItem.IsZero() {
		t.Errorf("NewestItem = %v, want zero time", result.NewestItem)
	}

	indexPath := filepath.Join(cfg.OutputDir, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		t.Errorf("expected %s to exist even with zero feeds: %v", indexPath, err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/renderer/ -v`
Expected: compile failure — `ExecuteWorkflow(cfg) used as value` / assignment mismatch.

- [ ] **Step 3: Change `ExecuteWorkflow`**

In `internal/renderer/workflow.go`, add the `Result` type just below `WorkflowConfig`:

```go
// Result summarizes what a single ExecuteWorkflow call produced. It feeds the
// multi-site index page.
type Result struct {
	FeedCount  int       // Feeds matching the time window and feed-list filter.
	ItemCount  int       // Items rendered, after min/max per-feed limits.
	NewestItem time.Time // Newest PublishedDate rendered; zero if no items.
}

// summarize computes a Result from the data about to be rendered.
func summarize(feeds []database.Feed, items map[string][]database.Item) *Result {
	result := &Result{FeedCount: len(feeds)}
	for i := range feeds {
		feedItems := items[feeds[i].URL]
		result.ItemCount += len(feedItems)
		for j := range feedItems {
			if feedItems[j].PublishedDate.After(result.NewestItem) {
				result.NewestItem = feedItems[j].PublishedDate
			}
		}
	}
	return result
}
```

Then change the signature and the two return paths. The function currently reads:

```go
func ExecuteWorkflow(config *WorkflowConfig) error {
```

Change to `func ExecuteWorkflow(config *WorkflowConfig) (*Result, error) {` and update **every** `return err` / `return fmt.Errorf(...)` inside it to `return nil, err` / `return nil, fmt.Errorf(...)`.

Replace the early-return block:

```go
	if len(feeds) == 0 {
		fmt.Println("No feeds found matching criteria") //nolint:forbidigo // User-facing output
		return nil
	}
```

with:

```go
	if len(feeds) == 0 {
		fmt.Println("No feeds found matching criteria") //nolint:forbidigo // User-facing output
	}
```

Note: **do not return early.** An empty site page is written so the multi-site index never links a missing file, and so a single-site no-match render no longer silently leaves the previous build in place.

Replace the final line:

```go
	// Generate site
	return generateSite(config, feeds, items, startTime, endTime)
```

with:

```go
	// Generate site.
	if err := generateSite(config, feeds, items, startTime, endTime); err != nil {
		return nil, err
	}

	return summarize(feeds, items), nil
```

- [ ] **Step 4: Update the caller**

In `cmd/render.go`, change:

```go
	// Execute the render operation
	return renderer.ExecuteWorkflow(renderConfig)
```

to:

```go
	// Execute the render operation.
	_, err := renderer.ExecuteWorkflow(renderConfig)
	return err
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/renderer/ ./... -v 2>&1 | tail -30`
Expected: PASS. If the zero-feed case panics inside `generateSite`, check `splitFeedsIntoPages` — with an empty slice and a positive page size it returns `nil`, giving `totalPages == 0`, which the `if totalPages > 1` guard already handles correctly.

- [ ] **Step 6: Verify and commit**

```bash
make format && make lint && make test
git add internal/renderer/ cmd/render.go
git commit -m "feat: ExecuteWorkflow returns render stats and always writes index.html"
```

---

## Task 4: `sitegroup` discovery and URL union

Turn a directory into an ordered list of sites, and flatten their URLs into a deduped union.

**Files:**
- Create: `internal/sitegroup/site.go`
- Test: `internal/sitegroup/site_test.go`

**Interfaces:**
- Consumes: `feedlist.LoadFeedList`, `feedlist.Format`, `feedlist.FormatOPML`, `feedlist.FormatText`, and `FeedList.Title()` from Task 1.
- Produces:
  ```go
  type Site struct {
      Slug   string
      Title  string
      Path   string
      Format feedlist.Format
      URLs   []string
  }
  type Skipped struct {
      Path string
      Err  error
  }
  func Discover(dir string) (sites []Site, skipped []Skipped, err error)
  func UnionURLs(sites []Site) []string
  ```
  `Discover` returns a non-nil `err` only for whole-directory failures: missing directory, no feed lists found, slug collision, unsafe slug. A file that fails to parse lands in `skipped` with `err == nil`.

- [ ] **Step 1: Write the failing tests**

Create `internal/sitegroup/site_test.go`:

```go
package sitegroup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lmorchard/feedspool-go/internal/feedlist"
)

const (
	feedA = "https://a.example.com/feed.xml"
	feedB = "https://b.example.com/feed.xml"
	feedC = "https://c.example.com/feed.xml"
)

// opmlWith builds a minimal OPML document with the given title and feed URLs.
func opmlWith(title string, urls ...string) string {
	doc := `<?xml version="1.0" encoding="UTF-8"?>` + "\n<opml version=\"2.0\">\n<head><title>" +
		title + "</title></head>\n<body>\n"
	for _, u := range urls {
		doc += `<outline text="x" type="rss" xmlUrl="` + u + `" />` + "\n"
	}
	return doc + "</body>\n</opml>\n"
}

// writeDir creates a temp directory containing the given name->content files.
func writeDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDiscoverMixedFormats(t *testing.T) {
	dir := writeDir(t, map[string]string{
		"tech-blogs.opml": opmlWith("Tech Blogs", feedA, feedB),
		"scratch.txt":     feedC + "\n",
		"README.md":       "ignore me",
	})
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o750); err != nil {
		t.Fatal(err)
	}

	sites, skipped, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(skipped) != 0 {
		t.Errorf("len(skipped) = %d, want 0", len(skipped))
	}
	if len(sites) != 2 {
		t.Fatalf("len(sites) = %d, want 2 (README.md and subdir must be ignored)", len(sites))
	}

	// Sorted by filename: scratch.txt before tech-blogs.opml.
	if sites[0].Slug != "scratch" {
		t.Errorf("sites[0].Slug = %q, want %q", sites[0].Slug, "scratch")
	}
	if sites[0].Title != "scratch" {
		t.Errorf("sites[0].Title = %q, want %q (text lists fall back to filename)", sites[0].Title, "scratch")
	}
	if sites[0].Format != feedlist.FormatText {
		t.Errorf("sites[0].Format = %v, want %v", sites[0].Format, feedlist.FormatText)
	}

	if sites[1].Slug != "tech-blogs" {
		t.Errorf("sites[1].Slug = %q, want %q", sites[1].Slug, "tech-blogs")
	}
	if sites[1].Title != "Tech Blogs" {
		t.Errorf("sites[1].Title = %q, want %q", sites[1].Title, "Tech Blogs")
	}
	if len(sites[1].URLs) != 2 {
		t.Errorf("len(sites[1].URLs) = %d, want 2", len(sites[1].URLs))
	}
}

func TestDiscoverSlugifiesFilename(t *testing.T) {
	dir := writeDir(t, map[string]string{
		"Tech Blogs & Friends.opml": opmlWith("", feedA),
	})

	sites, _, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if sites[0].Slug != "tech-blogs-friends" {
		t.Errorf("Slug = %q, want %q", sites[0].Slug, "tech-blogs-friends")
	}
	if sites[0].Title != "Tech Blogs & Friends" {
		t.Errorf("Title = %q, want the verbatim filename base", sites[0].Title)
	}
}

func TestDiscoverSlugCollision(t *testing.T) {
	dir := writeDir(t, map[string]string{
		"tech blogs.opml": opmlWith("A", feedA),
		"tech-blogs.opml": opmlWith("B", feedB),
	})

	_, _, err := Discover(dir)
	if err == nil {
		t.Fatal("Discover() error = nil, want a slug collision error")
	}
	for _, want := range []string{"tech blogs.opml", "tech-blogs.opml"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err.Error(), want)
		}
	}
}

func TestDiscoverEmptyDirectory(t *testing.T) {
	dir := writeDir(t, map[string]string{"notes.md": "hi"})

	_, _, err := Discover(dir)
	if err == nil {
		t.Fatal("Discover() error = nil, want an error for a directory with no feed lists")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("error %q does not name the directory", err.Error())
	}
}

func TestDiscoverMissingDirectory(t *testing.T) {
	_, _, err := Discover(filepath.Join(t.TempDir(), "nope"))
	if err == nil {
		t.Fatal("Discover() error = nil, want an error for a missing directory")
	}
}

func TestDiscoverPathIsFile(t *testing.T) {
	dir := writeDir(t, map[string]string{"a.opml": opmlWith("A", feedA)})

	_, _, err := Discover(filepath.Join(dir, "a.opml"))
	if err == nil {
		t.Fatal("Discover() error = nil, want an error when the path is a file")
	}
}

func TestDiscoverSkipsMalformedFile(t *testing.T) {
	dir := writeDir(t, map[string]string{
		"good.opml": opmlWith("Good", feedA),
		"bad.opml":  "<opml><head><title>unclosed",
	})

	sites, skipped, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover() error = %v, want nil (a bad file must not fail the run)", err)
	}
	if len(sites) != 1 || sites[0].Slug != "good" {
		t.Fatalf("sites = %+v, want only the good site", sites)
	}
	if len(skipped) != 1 {
		t.Fatalf("len(skipped) = %d, want 1", len(skipped))
	}
	if filepath.Base(skipped[0].Path) != "bad.opml" {
		t.Errorf("skipped[0].Path = %q, want bad.opml", skipped[0].Path)
	}
	if skipped[0].Err == nil {
		t.Error("skipped[0].Err = nil, want the parse error")
	}
}

func TestDiscoverEmptyFeedListIsStillASite(t *testing.T) {
	dir := writeDir(t, map[string]string{"empty.opml": opmlWith("Empty")})

	sites, _, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(sites) != 1 {
		t.Fatalf("len(sites) = %d, want 1", len(sites))
	}
	if len(sites[0].URLs) != 0 {
		t.Errorf("len(URLs) = %d, want 0", len(sites[0].URLs))
	}
}

func TestUnionURLs(t *testing.T) {
	sites := []Site{
		{Slug: "one", URLs: []string{feedA, feedB}},
		{Slug: "two", URLs: []string{feedB, feedC}},
		{Slug: "three", URLs: []string{feedA}},
	}

	got := UnionURLs(sites)
	want := []string{feedA, feedB, feedC}

	if len(got) != len(want) {
		t.Fatalf("UnionURLs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("UnionURLs()[%d] = %q, want %q (first-seen order)", i, got[i], want[i])
		}
	}
}

func TestUnionURLsEmpty(t *testing.T) {
	if got := UnionURLs(nil); len(got) != 0 {
		t.Errorf("UnionURLs(nil) = %v, want empty", got)
	}
}
```

The tests above use `strings.Contains`; add `"strings"` to the import block.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/sitegroup/ -v`
Expected: build failure — no non-test Go files in the package.

- [ ] **Step 3: Implement `site.go`**

Create `internal/sitegroup/site.go`:

```go
// Package sitegroup builds a group of feedspool sites from a directory of
// feed lists: one site per OPML or text file, plus a top-level index page.
package sitegroup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lmorchard/feedspool-go/internal/feedlist"
)

// Site is a single feed list discovered in a directory, along with everything
// needed to render it into its own subdirectory.
type Site struct {
	Slug   string          // Slugified filename base; the output subdirectory name.
	Title  string          // OPML head title, or the filename base if unset.
	Path   string          // Full path to the feed list file.
	Format feedlist.Format // Inferred from the file extension.
	URLs   []string        // Feed URLs in the list.
}

// Skipped records a feed list that could not be loaded. Discover returns these
// alongside the sites that did load, rather than failing the whole run.
type Skipped struct {
	Path string
	Err  error
}

// slugify converts a filename base into a safe output directory name:
// lowercase, with runs of non-alphanumeric characters collapsed to a hyphen
// and leading and trailing hyphens trimmed.
func slugify(name string) string {
	var b strings.Builder
	lastHyphen := false
	for _, r := range strings.ToLower(name) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// isFeedListExt reports whether the extension marks a supported feed list.
func isFeedListExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".opml", ".txt":
		return true
	default:
		return false
	}
}

// Discover scans dir for feed lists and returns one Site per loadable file,
// sorted by filename. A file that fails to parse is returned in skipped rather
// than failing the run. A non-nil error means the whole directory is unusable:
// it is missing, is not a directory, contains no feed lists, or two files
// produce the same slug.
func Discover(dir string) (sites []Site, skipped []Skipped, err error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read feed list directory %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("feed list path is not a directory: %s", dir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read feed list directory %s: %w", dir, err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !isFeedListExt(filepath.Ext(entry.Name())) {
			continue
		}
		names = append(names, entry.Name())
	}
	if len(names) == 0 {
		return nil, nil, fmt.Errorf("no .opml or .txt feed lists found in %s", dir)
	}
	sort.Strings(names)

	sites = make([]Site, 0, len(names))
	slugOwner := make(map[string]string, len(names))

	for _, name := range names {
		base := strings.TrimSuffix(name, filepath.Ext(name))
		slug := slugify(base)
		if slug == "" {
			return nil, nil, fmt.Errorf("feed list %s produces an empty directory name", name)
		}
		if owner, taken := slugOwner[slug]; taken {
			return nil, nil, fmt.Errorf(
				"feed lists %s and %s both produce the directory name %q", owner, name, slug)
		}
		slugOwner[slug] = name

		path := filepath.Join(dir, name)
		format := feedlist.DetectFormat(name)

		list, loadErr := feedlist.LoadFeedList(format, path)
		if loadErr != nil {
			skipped = append(skipped, Skipped{Path: path, Err: loadErr})
			continue
		}

		title := list.Title()
		if title == "" {
			title = base
		}

		sites = append(sites, Site{
			Slug:   slug,
			Title:  title,
			Path:   path,
			Format: format,
			URLs:   list.GetURLs(),
		})
	}

	return sites, skipped, nil
}

// UnionURLs flattens every site's feed URLs into a single deduped slice,
// preserving first-seen order. A feed listed by five sites is fetched once.
func UnionURLs(sites []Site) []string {
	seen := make(map[string]struct{})
	union := make([]string, 0)
	for i := range sites {
		for _, u := range sites[i].URLs {
			if _, dup := seen[u]; dup {
				continue
			}
			seen[u] = struct{}{}
			union = append(union, u)
		}
	}
	return union
}
```

`slugify` is a hand-rolled loop rather than a `regexp` — it avoids the dependency, is faster, and `gochecknoglobals` would reject a package-level compiled pattern here anyway.

Note the collision check runs **before** loading, so two colliding files error out even if one of them is malformed.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/sitegroup/ -v`
Expected: all tests PASS.

- [ ] **Step 5: Verify and commit**

```bash
make format && make lint && make test
git add internal/sitegroup/
git commit -m "feat: add sitegroup discovery and feed URL union"
```

---

## Task 5: Prune manifest

Deleting `comics.opml` must not leave `build/comics/` published but unlinked. Track generated slugs in a manifest and prune what disappears — but only ever what feedspool itself created.

**Files:**
- Create: `internal/sitegroup/manifest.go`
- Test: `internal/sitegroup/manifest_test.go`

**Interfaces:**
- Consumes: `Site` from Task 4.
- Produces:
  ```go
  const ManifestName = ".feedspool-sites.json"
  type Manifest struct {
      Version int      `json:"version"`
      Slugs   []string `json:"slugs"`
  }
  func ReadManifest(outputDir string) (*Manifest, error)
  func WriteManifest(outputDir string, sites []Site) error
  func Prune(outputDir string, sites []Site) (removed []string, err error)
  ```
  `ReadManifest` returns an empty `Manifest` (not an error) when the file is absent. `Prune` returns the slugs it removed.

- [ ] **Step 1: Write the failing tests**

Create `internal/sitegroup/manifest_test.go`:

```go
package sitegroup

import (
	"os"
	"path/filepath"
	"testing"
)

// mkdirs creates the given subdirectories under root.
func mkdirs(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := os.MkdirAll(filepath.Join(root, name), 0o750); err != nil {
			t.Fatal(err)
		}
	}
}

func TestManifestRoundTrip(t *testing.T) {
	out := t.TempDir()
	sites := []Site{{Slug: "comics"}, {Slug: "tech"}}

	if err := WriteManifest(out, sites); err != nil {
		t.Fatalf("WriteManifest() error = %v", err)
	}

	m, err := ReadManifest(out)
	if err != nil {
		t.Fatalf("ReadManifest() error = %v", err)
	}
	if len(m.Slugs) != 2 || m.Slugs[0] != "comics" || m.Slugs[1] != "tech" {
		t.Errorf("Slugs = %v, want [comics tech]", m.Slugs)
	}
	if m.Version != manifestVersion {
		t.Errorf("Version = %d, want %d", m.Version, manifestVersion)
	}
}

func TestReadManifestMissing(t *testing.T) {
	m, err := ReadManifest(t.TempDir())
	if err != nil {
		t.Fatalf("ReadManifest() error = %v, want nil for a missing manifest", err)
	}
	if len(m.Slugs) != 0 {
		t.Errorf("Slugs = %v, want empty", m.Slugs)
	}
}

func TestPruneRemovesDepartedSlug(t *testing.T) {
	out := t.TempDir()
	mkdirs(t, out, "comics", "tech")
	if err := WriteManifest(out, []Site{{Slug: "comics"}, {Slug: "tech"}}); err != nil {
		t.Fatal(err)
	}

	removed, err := Prune(out, []Site{{Slug: "tech"}})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(removed) != 1 || removed[0] != "comics" {
		t.Fatalf("removed = %v, want [comics]", removed)
	}
	if _, err := os.Stat(filepath.Join(out, "comics")); !os.IsNotExist(err) {
		t.Error("comics directory still exists after prune")
	}
	if _, err := os.Stat(filepath.Join(out, "tech")); err != nil {
		t.Errorf("tech directory was removed but is still discovered: %v", err)
	}
}

func TestPruneIgnoresUnknownDirectories(t *testing.T) {
	out := t.TempDir()
	mkdirs(t, out, "tech", "not-ours")
	if err := WriteManifest(out, []Site{{Slug: "tech"}}); err != nil {
		t.Fatal(err)
	}

	removed, err := Prune(out, []Site{{Slug: "tech"}})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want empty", removed)
	}
	if _, err := os.Stat(filepath.Join(out, "not-ours")); err != nil {
		t.Error("Prune removed a directory that was never in the manifest")
	}
}

func TestPruneWithoutManifestIsNoOp(t *testing.T) {
	out := t.TempDir()
	mkdirs(t, out, "leftover")

	removed, err := Prune(out, []Site{{Slug: "tech"}})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want empty", removed)
	}
	if _, err := os.Stat(filepath.Join(out, "leftover")); err != nil {
		t.Error("Prune removed a directory with no manifest present")
	}
}

func TestPruneRejectsUnsafeSlug(t *testing.T) {
	out := t.TempDir()
	victim := filepath.Join(out, "victim")
	mkdirs(t, out, "victim")

	// Hand-write a manifest containing a traversal attempt.
	manifest := `{"version":1,"slugs":["../victim","tech/nested",".",""]}`
	if err := os.WriteFile(filepath.Join(out, ManifestName), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	removed, err := Prune(out, nil)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want empty for unsafe slugs", removed)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Error("Prune followed a path traversal out of the output directory")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/sitegroup/ -run 'Manifest|Prune' -v`
Expected: compile failure — `undefined: WriteManifest`.

- [ ] **Step 3: Implement `manifest.go`**

Create `internal/sitegroup/manifest.go`:

```go
package sitegroup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/lmorchard/feedspool-go/internal/config"
)

// ManifestName is the file, written in the output root, recording which site
// directories feedspool generated on the previous run.
const ManifestName = ".feedspool-sites.json"

// manifestVersion is the current on-disk manifest schema version.
const manifestVersion = 1

// manifestPerm is the file mode for the manifest.
const manifestPerm fs.FileMode = 0o600

// Manifest records the site directories generated by the previous run so the
// next run can prune the ones that disappeared.
type Manifest struct {
	Version int      `json:"version"`
	Slugs   []string `json:"slugs"`
}

// ReadManifest loads the manifest from outputDir. A missing manifest is not an
// error: it yields an empty Manifest, meaning there is nothing to prune.
func ReadManifest(outputDir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(outputDir, ManifestName)) //nolint:gosec // Path is operator-supplied.
	if errors.Is(err, fs.ErrNotExist) {
		return &Manifest{Version: manifestVersion}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", ManifestName, err)
	}

	m := &Manifest{}
	if err := json.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", ManifestName, err)
	}
	return m, nil
}

// WriteManifest records the slugs of the given sites in outputDir.
func WriteManifest(outputDir string, sites []Site) error {
	slugs := make([]string, 0, len(sites))
	for i := range sites {
		slugs = append(slugs, sites[i].Slug)
	}

	data, err := json.Marshal(&Manifest{Version: manifestVersion, Slugs: slugs})
	if err != nil {
		return fmt.Errorf("failed to encode %s: %w", ManifestName, err)
	}

	if err := os.MkdirAll(outputDir, config.DefaultDirPerm); err != nil {
		return fmt.Errorf("failed to create output directory %s: %w", outputDir, err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, ManifestName), data, manifestPerm); err != nil {
		return fmt.Errorf("failed to write %s: %w", ManifestName, err)
	}
	return nil
}

// isSafeSlug reports whether slug names a direct child of an output directory.
// It rejects empty names, dot names, and anything containing a separator, so a
// hand-edited or corrupted manifest cannot make Prune escape the output root.
func isSafeSlug(slug string) bool {
	if slug == "" || slug == "." || slug == ".." {
		return false
	}
	return slug == filepath.Base(slug) && !filepath.IsAbs(slug)
}

// Prune removes site directories that the previous run generated but that are
// no longer discovered. It keys off discovered sites, not successfully
// rendered ones, so a site that failed to render is never deleted.
//
// A directory is removed only if it is named in the previous manifest, is a
// safe direct child of outputDir, and exists as a directory. Prune can
// therefore never touch anything feedspool did not create.
func Prune(outputDir string, sites []Site) (removed []string, err error) {
	previous, err := ReadManifest(outputDir)
	if err != nil {
		return nil, err
	}

	current := make(map[string]struct{}, len(sites))
	for i := range sites {
		current[sites[i].Slug] = struct{}{}
	}

	for _, slug := range previous.Slugs {
		if _, kept := current[slug]; kept {
			continue
		}
		if !isSafeSlug(slug) {
			continue
		}

		path := filepath.Join(outputDir, slug)
		info, statErr := os.Stat(path)
		if statErr != nil || !info.IsDir() {
			continue
		}
		if rmErr := os.RemoveAll(path); rmErr != nil {
			return removed, fmt.Errorf("failed to remove stale site directory %s: %w", path, rmErr)
		}
		removed = append(removed, slug)
	}

	return removed, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/sitegroup/ -v`
Expected: all tests PASS.

- [ ] **Step 5: Verify and commit**

```bash
make format && make lint && make test
git add internal/sitegroup/
git commit -m "feat: add site manifest and stale directory pruning"
```

---

## Task 6: Index page template, assets, and `RenderSiteIndex`

**Files:**
- Create: `internal/renderer/siteindex.go`
- Create: `internal/renderer/templates/site-index.html`
- Create: `internal/renderer/assets/site-index.css`
- Create: `internal/renderer/assets/css/site-index.css`
- Create: `internal/renderer/assets/site-index.js`
- Test: `internal/renderer/siteindex_test.go`

**Interfaces:**
- Consumes: existing `Renderer`, `NewRenderer`, `Renderer.Render`, `Renderer.CopyAssets`.
- Produces:
  ```go
  type SiteEntry struct {
      Slug       string
      Title      string
      FeedCount  int
      ItemCount  int
      NewestItem time.Time
  }
  type SiteIndexContext struct {
      Sites       []SiteEntry
      GeneratedAt time.Time
      TimeWindow  string
  }
  func RenderSiteIndex(outputDir, templatesDir, assetsDir string, ctx *SiteIndexContext) error
  ```

**Implementation note on assets:** `Renderer.CopyAssets` walks the entire embedded assets tree, so adding `site-index.css`/`site-index.js` there means every per-site directory also gets those two small files, and the output root gets the full bundle. That is intentional — reusing `CopyAssets` avoids a second embed and a selective copier, and keeps `init --extract-assets` working uniformly for both bundles. The waste is a few KB.

- [ ] **Step 1: Write the failing test**

Create `internal/renderer/siteindex_test.go`:

```go
package renderer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenderSiteIndex(t *testing.T) {
	out := t.TempDir()
	newest := time.Date(2026, 8, 22, 9, 14, 0, 0, time.UTC)

	ctx := &SiteIndexContext{
		Sites: []SiteEntry{
			{Slug: "tech-blogs", Title: "Tech Blogs", FeedCount: 42, ItemCount: 118, NewestItem: newest},
			{Slug: "local-news", Title: "Local News", FeedCount: 15, ItemCount: 0},
		},
		GeneratedAt: newest,
		TimeWindow:  "Last 24h",
	}

	if err := RenderSiteIndex(out, "", "", ctx); err != nil {
		t.Fatalf("RenderSiteIndex() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatalf("reading index.html: %v", err)
	}
	html := string(data)

	for _, want := range []string{
		`href="tech-blogs/"`,
		`href="local-news/"`,
		"Tech Blogs",
		"Local News",
		"42 feeds",
		"118 new items",
		"site-entry-quiet",
		`datetime="2026-08-22T09:14:00Z"`,
		"Last 24h",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("index.html does not contain %q", want)
		}
	}

	// A site with no items must not emit a bogus zero-time <time> element.
	if strings.Contains(html, "0001-01-01") {
		t.Error("index.html rendered a zero timestamp for a site with no items")
	}

	// Assets must land in the output root.
	for _, name := range []string{"site-index.css", "site-index.js"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Errorf("expected %s in output root: %v", name, err)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/renderer/ -run RenderSiteIndex -v`
Expected: compile failure — `undefined: SiteIndexContext`.

- [ ] **Step 3: Create the template**

Create `internal/renderer/templates/site-index.html`:

```html
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>feedspool</title>
    <link rel="stylesheet" href="site-index.css">
    <script type="module" src="site-index.js"></script>
</head>
<body>
    <header>
        <h1>feedspool</h1>
        <p class="site-index-meta">{{.TimeWindow}}</p>
    </header>

    <time-formatter>
    <main>
        {{if .Sites}}
        <ul class="site-list">
            {{range .Sites}}
            <li class="site-entry{{if eq .ItemCount 0}} site-entry-quiet{{end}}">
                <a class="site-link" href="{{.Slug}}/">{{.Title}}</a>
                <p class="site-stats">
                    <span class="site-stat">{{.FeedCount}} feeds</span>
                    <span class="site-stat">{{.ItemCount}} new items</span>
                    {{if not .NewestItem.IsZero}}
                    <span class="site-stat">newest
                        <time datetime="{{.NewestItem.Format "2006-01-02T15:04:05Z07:00"}}">{{.NewestItem.Format "Jan 2, 2006 15:04 UTC"}}</time>
                    </span>
                    {{end}}
                </p>
            </li>
            {{end}}
        </ul>
        {{else}}
        <p class="site-list-empty">No feed lists found.</p>
        {{end}}
    </main>
    </time-formatter>

    <footer>
        <p>Generated
            <time datetime="{{.GeneratedAt.Format "2006-01-02T15:04:05Z07:00"}}">{{.GeneratedAt.Format "Jan 2, 2006 15:04 UTC"}}</time>
        </p>
    </footer>
</body>
</html>
```

Timestamps are emitted as absolute ISO inside `<time datetime>` and upgraded to relative time in the reader's timezone by the existing `<time-formatter>` custom element. Baking "22m ago" into static HTML at build time would be wrong a minute later.

- [ ] **Step 4: Create the assets**

Create `internal/renderer/assets/site-index.css`:

```css
/* Site Index Styles - Entry Point for the multi-site directory page */

@import "css/variables.css";
@import "css/base.css";
@import "css/site-index.css";
```

Create `internal/renderer/assets/css/site-index.css`:

```css
/* Multi-site directory index */

.site-index-meta {
    color: var(--text-secondary);
    font-size: 0.9rem;
}

.site-list {
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: 12px;
    margin: 16px 0;
}

.site-entry {
    background-color: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: 6px;
    padding: 12px 16px;
}

.site-link {
    color: var(--link-color);
    font-size: 1.2rem;
    font-weight: 600;
    text-decoration: none;
}

.site-link:hover {
    text-decoration: underline;
}

.site-stats {
    color: var(--text-secondary);
    font-size: 0.85rem;
    margin-top: 4px;
}

.site-stat + .site-stat::before {
    content: " · ";
}

.site-entry-quiet {
    opacity: 0.6;
}

.site-list-empty {
    color: var(--text-secondary);
    margin: 16px 0;
}
```

Before committing, open `internal/renderer/assets/css/variables.css` and confirm the custom property names used above (`--text-secondary`, `--bg-secondary`, `--border-color`, `--link-color`) actually exist. If any differ, use the real names — a typo'd custom property fails silently.

Create `internal/renderer/assets/site-index.js`:

```js
/**
 * Site Index JavaScript Module
 * Entry point for the multi-site directory page.
 *
 * Registers only <time-formatter>, which upgrades absolute <time datetime>
 * values into relative times in the reader's timezone. The feed reader's
 * other custom elements are not used on this page.
 */

import './js/time-formatter.js';
```

- [ ] **Step 5: Implement `siteindex.go`**

Create `internal/renderer/siteindex.go`:

```go
package renderer

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lmorchard/feedspool-go/internal/config"
)

// siteIndexTemplate is the template name for the multi-site directory page.
const siteIndexTemplate = "site-index.html"

// SiteEntry is one site's row on the multi-site index page.
type SiteEntry struct {
	Slug       string
	Title      string
	FeedCount  int
	ItemCount  int
	NewestItem time.Time // Zero when the site rendered no items.
}

// SiteIndexContext is the data passed to the site-index template.
type SiteIndexContext struct {
	Sites       []SiteEntry
	GeneratedAt time.Time
	TimeWindow  string
}

// RenderSiteIndex writes the multi-site directory page into outputDir and
// copies the static assets it needs alongside it.
func RenderSiteIndex(outputDir, templatesDir, assetsDir string, ctx *SiteIndexContext) error {
	if err := os.MkdirAll(outputDir, config.DefaultDirPerm); err != nil {
		return fmt.Errorf("failed to create output directory %s: %w", outputDir, err)
	}

	r := NewRenderer(templatesDir, assetsDir)

	outputFile := filepath.Join(outputDir, "index.html")
	file, err := os.Create(outputFile) //nolint:gosec // Path is operator-supplied.
	if err != nil {
		return fmt.Errorf("failed to create site index %s: %w", outputFile, err)
	}
	defer file.Close()

	if err := r.Render(file, siteIndexTemplate, ctx); err != nil {
		return fmt.Errorf("failed to render site index: %w", err)
	}

	if err := r.CopyAssets(outputDir); err != nil {
		return fmt.Errorf("failed to copy site index assets: %w", err)
	}

	return nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/renderer/ -v`
Expected: PASS.

If `RenderSiteIndex` fails with a template load error, check `LoadTemplateFromFS` — it reads `iframe_content.html` first and falls back to the embedded copy, so a custom templates directory without that file still works. No change needed; this note is for debugging.

- [ ] **Step 7: Verify and commit**

```bash
make format && make lint && make test
git add internal/renderer/
git commit -m "feat: add multi-site index template, assets, and renderer"
```

---

## Task 7: `sitegroup` orchestration

Tie discovery, fetch planning, per-site rendering, pruning, and the index together.

**Files:**
- Create: `internal/sitegroup/render.go`
- Test: `internal/sitegroup/render_test.go`

**Interfaces:**
- Consumes: `Discover`, `UnionURLs` (Task 4); `Prune`, `WriteManifest` (Task 5); `renderer.ExecuteWorkflow`, `renderer.Result`, `renderer.WorkflowConfig` (Task 3); `renderer.RenderSiteIndex`, `renderer.SiteIndexContext`, `renderer.SiteEntry` (Task 6).
- Produces:
  ```go
  var ErrPartialFailure = errors.New("...")
  type FetchPlan struct {
      Sites      []Site
      Skipped    []Skipped
      URLs       []string
      References int
  }
  func PlanFetch(dir string) (*FetchPlan, error)

  type SiteResult struct {
      Site
      renderer.Result
      Err error
  }
  type RenderSummary struct {
      Sites   []SiteResult
      Skipped []Skipped
      Removed []string
  }
  func (s *RenderSummary) HasFailures() bool
  func RenderAll(dir string, base *renderer.WorkflowConfig) (*RenderSummary, error)
  ```
  `RenderAll` returns a non-nil `error` only for fatal problems (discovery failure, prune failure, index render failure). Per-site failures live in `SiteResult.Err`; callers check `HasFailures()` and return `ErrPartialFailure`.

- [ ] **Step 1: Write the failing tests**

Create `internal/sitegroup/render_test.go`:

```go
package sitegroup

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/renderer"
)

// newTestDB creates an initialized database containing feedA and feedB, each
// with one recent item, and returns its path.
func newTestDB(t *testing.T) string {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "sitegroup_test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.InitSchema(); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	for i, u := range []string{feedA, feedB} {
		if err := db.UpsertFeed(&database.Feed{
			URL:                 u,
			Title:               "Feed",
			LastFetchTime:       now,
			LastSuccessfulFetch: now,
		}); err != nil {
			t.Fatal(err)
		}
		if err := db.UpsertItem(&database.Item{
			FeedURL:       u,
			GUID:          string(rune('a' + i)),
			Title:         "Item",
			Link:          u + "/item",
			PublishedDate: now.Add(-time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}

	return dbPath
}

func TestPlanFetchDedupes(t *testing.T) {
	dir := writeDir(t, map[string]string{
		"one.opml": opmlWith("One", feedA, feedB),
		"two.opml": opmlWith("Two", feedB, feedC),
	})

	plan, err := PlanFetch(dir)
	if err != nil {
		t.Fatalf("PlanFetch() error = %v", err)
	}
	if len(plan.URLs) != 3 {
		t.Errorf("len(URLs) = %d, want 3", len(plan.URLs))
	}
	if plan.References != 4 {
		t.Errorf("References = %d, want 4", plan.References)
	}
	if len(plan.Sites) != 2 {
		t.Errorf("len(Sites) = %d, want 2", len(plan.Sites))
	}
}

func TestRenderAllBuildsSitesAndIndex(t *testing.T) {
	dir := writeDir(t, map[string]string{
		"tech.opml":   opmlWith("Tech", feedA),
		"comics.opml": opmlWith("Comics", feedA, feedB),
	})
	out := filepath.Join(t.TempDir(), "build")

	summary, err := RenderAll(dir, &renderer.WorkflowConfig{
		MaxAge:    "24h",
		OutputDir: out,
		Database:  newTestDB(t),
	})
	if err != nil {
		t.Fatalf("RenderAll() error = %v", err)
	}
	if summary.HasFailures() {
		t.Fatalf("summary has failures: %+v", summary.Sites)
	}
	if len(summary.Sites) != 2 {
		t.Fatalf("len(Sites) = %d, want 2", len(summary.Sites))
	}

	// Sorted by filename: comics before tech.
	if summary.Sites[0].Slug != "comics" {
		t.Errorf("Sites[0].Slug = %q, want comics", summary.Sites[0].Slug)
	}
	if summary.Sites[0].FeedCount != 2 {
		t.Errorf("comics FeedCount = %d, want 2", summary.Sites[0].FeedCount)
	}
	if summary.Sites[1].FeedCount != 1 {
		t.Errorf("tech FeedCount = %d, want 1", summary.Sites[1].FeedCount)
	}

	for _, path := range []string{
		filepath.Join(out, "index.html"),
		filepath.Join(out, "comics", "index.html"),
		filepath.Join(out, "tech", "index.html"),
		filepath.Join(out, ManifestName),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected %s to exist: %v", path, err)
		}
	}
}

func TestRenderAllPrunesRemovedList(t *testing.T) {
	dbPath := newTestDB(t)
	out := filepath.Join(t.TempDir(), "build")

	dir := writeDir(t, map[string]string{
		"tech.opml":   opmlWith("Tech", feedA),
		"comics.opml": opmlWith("Comics", feedB),
	})
	base := &renderer.WorkflowConfig{MaxAge: "24h", OutputDir: out, Database: dbPath}

	if _, err := RenderAll(dir, base); err != nil {
		t.Fatalf("first RenderAll() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "comics")); err != nil {
		t.Fatalf("comics was not built: %v", err)
	}

	if err := os.Remove(filepath.Join(dir, "comics.opml")); err != nil {
		t.Fatal(err)
	}

	summary, err := RenderAll(dir, base)
	if err != nil {
		t.Fatalf("second RenderAll() error = %v", err)
	}
	if len(summary.Removed) != 1 || summary.Removed[0] != "comics" {
		t.Errorf("Removed = %v, want [comics]", summary.Removed)
	}
	if _, err := os.Stat(filepath.Join(out, "comics")); !os.IsNotExist(err) {
		t.Error("comics directory survived pruning")
	}
}

func TestRenderAllReportsSkippedFiles(t *testing.T) {
	dir := writeDir(t, map[string]string{
		"good.opml": opmlWith("Good", feedA),
		"bad.opml":  "<opml><head><title>unclosed",
	})
	out := filepath.Join(t.TempDir(), "build")

	summary, err := RenderAll(dir, &renderer.WorkflowConfig{
		MaxAge:    "24h",
		OutputDir: out,
		Database:  newTestDB(t),
	})
	if err != nil {
		t.Fatalf("RenderAll() error = %v", err)
	}
	if len(summary.Skipped) != 1 {
		t.Fatalf("len(Skipped) = %d, want 1", len(summary.Skipped))
	}
	if !summary.HasFailures() {
		t.Error("HasFailures() = false, want true when a file was skipped")
	}
	if _, err := os.Stat(filepath.Join(out, "good", "index.html")); err != nil {
		t.Errorf("the good site should still have built: %v", err)
	}
}

func TestRenderAllCleanRemovesOutputRootOnce(t *testing.T) {
	dbPath := newTestDB(t)
	out := filepath.Join(t.TempDir(), "build")
	dir := writeDir(t, map[string]string{"tech.opml": opmlWith("Tech", feedA)})

	if err := os.MkdirAll(out, 0o750); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(out, "stale.html")
	if err := os.WriteFile(stale, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := RenderAll(dir, &renderer.WorkflowConfig{
		MaxAge:    "24h",
		OutputDir: out,
		Database:  dbPath,
		Clean:     true,
	}); err != nil {
		t.Fatalf("RenderAll() error = %v", err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("--clean did not remove pre-existing output root contents")
	}
	if _, err := os.Stat(filepath.Join(out, "tech", "index.html")); err != nil {
		t.Errorf("site was not built after clean: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/sitegroup/ -run 'PlanFetch|RenderAll' -v`
Expected: compile failure — `undefined: PlanFetch`.

- [ ] **Step 3: Implement `render.go`**

Create `internal/sitegroup/render.go`:

```go
package sitegroup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lmorchard/feedspool-go/internal/renderer"
	"github.com/sirupsen/logrus"
)

// ErrPartialFailure reports that the run completed but some feed lists were
// skipped or failed to render. Callers should surface a non-zero exit status
// so cron and CI notice, even though a site was still published.
var ErrPartialFailure = errors.New("one or more feed lists were skipped or failed to render")

// FetchPlan is the deduped work for a directory-mode fetch.
type FetchPlan struct {
	Sites      []Site
	Skipped    []Skipped
	URLs       []string // Deduped union across every site.
	References int      // Total URL references before dedup.
}

// PlanFetch discovers the feed lists in dir and unions their URLs so a feed
// referenced by several lists is fetched exactly once.
func PlanFetch(dir string) (*FetchPlan, error) {
	sites, skipped, err := Discover(dir)
	if err != nil {
		return nil, err
	}

	references := 0
	for i := range sites {
		references += len(sites[i].URLs)
	}

	return &FetchPlan{
		Sites:      sites,
		Skipped:    skipped,
		URLs:       UnionURLs(sites),
		References: references,
	}, nil
}

// SiteResult is the outcome of rendering one site.
type SiteResult struct {
	Site
	renderer.Result
	Err error // Non-nil if this site failed to render.
}

// RenderSummary is the outcome of a whole directory-mode render.
type RenderSummary struct {
	Sites   []SiteResult
	Skipped []Skipped
	Removed []string // Slugs whose stale directories were pruned.
}

// HasFailures reports whether any feed list was skipped or failed to render.
func (s *RenderSummary) HasFailures() bool {
	if len(s.Skipped) > 0 {
		return true
	}
	for i := range s.Sites {
		if s.Sites[i].Err != nil {
			return true
		}
	}
	return false
}

// RenderAll renders one site per discovered feed list into a subdirectory of
// base.OutputDir, prunes directories for lists that disappeared, and writes the
// top-level index page.
//
// base supplies the render settings shared by every site. Its OutputDir is the
// root under which each site's directory is created, and its Clean flag applies
// to that root only, once, up front.
//
// A non-nil error means the run could not proceed. Per-site failures are
// reported in the returned summary instead, so one bad list never sinks a
// scheduled publish.
func RenderAll(dir string, base *renderer.WorkflowConfig) (*RenderSummary, error) {
	if base.Clean {
		if err := os.RemoveAll(base.OutputDir); err != nil {
			return nil, fmt.Errorf("failed to clean output directory %s: %w", base.OutputDir, err)
		}
	}

	sites, skipped, err := Discover(dir)
	if err != nil {
		return nil, err
	}
	for _, s := range skipped {
		logrus.Warnf("Skipping feed list %s: %v", s.Path, s.Err)
	}

	summary := &RenderSummary{
		Sites:   make([]SiteResult, 0, len(sites)),
		Skipped: skipped,
	}

	for i := range sites {
		summary.Sites = append(summary.Sites, renderOneSite(&sites[i], base))
	}

	removed, err := Prune(base.OutputDir, sites)
	if err != nil {
		return nil, err
	}
	summary.Removed = removed
	for _, slug := range removed {
		logrus.Infof("Pruned stale site directory: %s", slug)
	}

	if err := WriteManifest(base.OutputDir, sites); err != nil {
		return nil, err
	}

	if err := renderIndex(base, summary); err != nil {
		return nil, err
	}

	logrus.Infof("Generated %d sites in %s", len(summary.Sites), base.OutputDir)
	return summary, nil
}

// renderOneSite renders a single site into its own subdirectory. A render
// failure is recorded on the result rather than returned, so the remaining
// sites still build.
func renderOneSite(site *Site, base *renderer.WorkflowConfig) SiteResult {
	cfg := *base // Copy; every field is a value type.
	cfg.OutputDir = filepath.Join(base.OutputDir, site.Slug)
	cfg.FeedsFile = site.Path
	cfg.Format = string(site.Format)
	cfg.Clean = false // The output root was already cleaned once, up front.

	logrus.Infof("Rendering site %q from %s", site.Title, site.Path)

	result, err := renderer.ExecuteWorkflow(&cfg)
	if err != nil {
		logrus.Warnf("Failed to render site %q: %v", site.Title, err)
		return SiteResult{Site: *site, Err: err}
	}

	return SiteResult{Site: *site, Result: *result}
}

// renderIndex writes the top-level directory page. A site is listed only if its
// index.html exists on disk: a site that failed this run but built previously
// stays linked with slightly stale content, and one that never built is omitted
// rather than becoming a dead link.
func renderIndex(base *renderer.WorkflowConfig, summary *RenderSummary) error {
	entries := make([]renderer.SiteEntry, 0, len(summary.Sites))
	for i := range summary.Sites {
		s := &summary.Sites[i]
		if _, err := os.Stat(filepath.Join(base.OutputDir, s.Slug, "index.html")); err != nil {
			logrus.Warnf("Omitting %q from the index: no rendered page on disk", s.Title)
			continue
		}
		entries = append(entries, renderer.SiteEntry{
			Slug:       s.Slug,
			Title:      s.Title,
			FeedCount:  s.FeedCount,
			ItemCount:  s.ItemCount,
			NewestItem: s.NewestItem,
		})
	}

	return renderer.RenderSiteIndex(base.OutputDir, base.TemplatesDir, base.AssetsDir,
		&renderer.SiteIndexContext{
			Sites:       entries,
			GeneratedAt: time.Now().UTC(),
			TimeWindow:  timeWindowLabel(base),
		})
}

// timeWindowLabel describes the render window the same way the per-site pages
// do, so the index and the sites agree.
func timeWindowLabel(base *renderer.WorkflowConfig) string {
	if base.MaxAge != "" {
		return fmt.Sprintf("Last %s", base.MaxAge)
	}
	if base.Start != "" || base.End != "" {
		return fmt.Sprintf("From %s to %s", base.Start, base.End)
	}
	return ""
}
```

Note `SiteResult` embeds both `Site` and `renderer.Result`, so `s.Slug`, `s.Title`, `s.FeedCount`, `s.ItemCount`, and `s.NewestItem` are all promoted. Neither struct has a field name in common, so there is no ambiguity.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/sitegroup/ -v`
Expected: all tests PASS.

If `TestRenderAllPrunesRemovedList` fails because `comics` survived, check that `Prune` runs **after** the render loop and receives `sites` (discovered), not the filtered index entries.

- [ ] **Step 5: Verify and commit**

```bash
make format && make lint && make test
git add internal/sitegroup/
git commit -m "feat: add sitegroup fetch planning and multi-site render orchestration"
```

---

## Task 8: Config and `--feeds-dir` on `fetch` and `render`

**Files:**
- Modify: `internal/config/config.go` (`FeedListConfig` ~line 45, `LoadConfig` ~line 97, `GetDefault` ~line 135, `HasDefaultFeedList` ~line 185)
- Modify: `internal/config/config_test.go`
- Modify: `cmd/fetch.go`
- Modify: `cmd/render.go`
- Modify: `.golangci.yml`

**Interfaces:**
- Consumes: `sitegroup.PlanFetch`, `sitegroup.RenderAll`, `sitegroup.ErrPartialFailure`; `Orchestrator.FetchFromURLs`.
- Produces:
  ```go
  // internal/config
  type FeedListConfig struct {
      Format   string
      Filename string
      Dir      string
  }
  func (c *Config) HasFeedListDir() bool
  ```
  Plus a `--feeds-dir` string flag on both `fetch` and `render`.

- [ ] **Step 1: Write the failing config test**

Append to `internal/config/config_test.go`:

```go
func TestConfigValidateAmbiguousFeedList(t *testing.T) {
	cfg := Config{FeedList: FeedListConfig{Dir: "./opml", Filename: "feeds.opml"}}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want an error when both dir and filename are set")
	}
	for _, want := range []string{"feedlist.dir", "feedlist.filename"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err.Error(), want)
		}
	}
}

func TestConfigValidateAccepts(t *testing.T) {
	cases := []Config{
		{},
		{FeedList: FeedListConfig{Dir: "./opml"}},
		{FeedList: FeedListConfig{Format: "opml", Filename: "feeds.opml"}},
	}

	for i := range cases {
		if err := cases[i].Validate(); err != nil {
			t.Errorf("case %d: Validate() = %v, want nil", i, err)
		}
	}
}

func TestHasFeedListDir(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"empty", Config{}, false},
		{"dir set", Config{FeedList: FeedListConfig{Dir: "./opml"}}, true},
		{"filename only", Config{FeedList: FeedListConfig{Format: "opml", Filename: "a.opml"}}, false},
	}

	for _, tt := range tests {
		if got := tt.cfg.HasFeedListDir(); got != tt.want {
			t.Errorf("%s: HasFeedListDir() = %v, want %v", tt.name, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run 'HasFeedListDir|ConfigValidate' -v`
Expected: compile failure — `unknown field Dir`, `HasFeedListDir undefined`, `Validate undefined`.

Add `"strings"` to the test file's imports if it is not already there.

- [ ] **Step 3: Add `Dir` to config**

In `internal/config/config.go`:

```go
type FeedListConfig struct {
	Format   string
	Filename string
	Dir      string
}
```

In `LoadConfig`, extend the `FeedList` literal:

```go
		FeedList: FeedListConfig{
			Format:   viper.GetString("feedlist.format"),
			Filename: viper.GetString("feedlist.filename"),
			Dir:      viper.GetString("feedlist.dir"),
		},
```

In `GetDefault`, extend the `FeedList` literal with `Dir: ""`.

Next to `HasDefaultFeedList`, add:

```go
// HasFeedListDir returns true if a feed list directory is configured.
func (c *Config) HasFeedListDir() bool {
	return c.FeedList.Dir != ""
}

// Validate reports configuration that cannot be acted on unambiguously.
func (c *Config) Validate() error {
	if c.FeedList.Dir != "" && c.FeedList.Filename != "" {
		return errors.New(
			"ambiguous config: set either feedlist.dir or feedlist.filename, not both")
	}
	return nil
}
```

Add `"errors"` to the imports in `internal/config/config.go`.

- [ ] **Step 4: Run config test to verify it passes**

Run: `go test ./internal/config/ -v`
Expected: PASS.

- [ ] **Step 5: Add `--feeds-dir` to `fetch`**

In `cmd/fetch.go`, add `fetchFeedsDir string` to the `var` block and register the flag in `init`:

```go
	fetchCmd.Flags().StringVar(&fetchFeedsDir, "feeds-dir", "",
		"Directory of OPML/text feed lists; fetches the deduped union of all of them")
```

In `runFetch`, insert the directory branch **before** the existing file/database dispatch, right after the single-URL check:

```go
	// Determine fetch mode and execute.
	if len(args) == 1 {
		return runSingleURLFetch(ctx, orchestrator, args[0], opts, cfg)
	}

	// An explicit --filename/--format overrides a configured feedlist.dir:
	// single-file mode wins when you ask for it directly. But combining the
	// --feeds-dir flag with them is a contradiction, not a precedence question.
	explicitFile := fetchFormat != "" || fetchFilename != ""

	if fetchFeedsDir != "" && explicitFile {
		return errors.New("--feeds-dir cannot be combined with --format or --filename")
	}

	feedsDir := fetchFeedsDir
	if feedsDir == "" && !explicitFile {
		feedsDir = cfg.FeedList.Dir
	}
	if feedsDir != "" {
		return runDirFetch(ctx, orchestrator, feedsDir, opts, cfg)
	}

	if explicitFile || cfg.HasDefaultFeedList() {
		return runFileFetch(ctx, orchestrator, opts, cfg)
	}

	// Database mode (no args, no file flags, no config defaults).
	return runDatabaseFetch(ctx, orchestrator, opts, cfg)
```

Add the new handler next to `runFileFetch`:

```go
func runDirFetch(
	ctx context.Context, orchestrator *fetcher.Orchestrator, dir string,
	opts fetcher.FetchOptions, cfg *config.Config,
) error {
	plan, err := sitegroup.PlanFetch(dir)
	if err != nil {
		return err
	}

	for _, s := range plan.Skipped {
		logrus.Warnf("Skipping feed list %s: %v", s.Path, s.Err)
	}

	logrus.Infof("Found %d feed lists, %d unique feeds (%d references)",
		len(plan.Sites), len(plan.URLs), plan.References)

	results := orchestrator.FetchFromURLs(ctx, plan.URLs, opts)

	summary := fetcher.ProcessResults(results)
	summary.Mode = "dir"
	summary.Print(cfg)

	if len(plan.Skipped) > 0 {
		return sitegroup.ErrPartialFailure
	}
	return nil
}
```

Add `"errors"` and `"github.com/lmorchard/feedspool-go/internal/sitegroup"` to the imports.

Also validate the config at the top of `runFetch`, right after `cfg := GetConfig()`:

```go
	if err := cfg.Validate(); err != nil {
		return err
	}
```

**Note on `--remove-missing`:** it receives `plan.URLs`, the union. This is the only correct semantics — running remove-missing per file in a loop would have each list delete the other lists' feeds. No extra code is needed; the phase split makes it correct by construction.

- [ ] **Step 6: Add `--feeds-dir` to `render`**

In `cmd/render.go`, add `renderFeedsDir string` to the `var` block and register:

```go
	renderCmd.Flags().StringVar(&renderFeedsDir, "feeds-dir", "",
		"Directory of OPML/text feed lists; builds one site per list plus an index")
```

Rewrite `runRender`:

```go
func runRender(_ *cobra.Command, _ []string) error {
	cfg := GetConfig()
	if err := cfg.Validate(); err != nil {
		return err
	}

	// Build configuration from flags and config file.
	renderConfig := buildRenderConfig(cfg)

	// An explicit --feeds overrides a configured feedlist.dir; combining it
	// with the --feeds-dir flag is a contradiction.
	if renderFeedsDir != "" && renderFeeds != "" {
		return errors.New("--feeds-dir cannot be combined with --feeds")
	}

	feedsDir := renderFeedsDir
	if feedsDir == "" && renderFeeds == "" {
		feedsDir = cfg.FeedList.Dir
	}
	if feedsDir != "" {
		return runDirRender(feedsDir, renderConfig)
	}

	// Validate configuration.
	if err := validateRenderConfig(renderConfig); err != nil {
		return err
	}

	// Execute the render operation.
	_, err := renderer.ExecuteWorkflow(renderConfig)
	return err
}

func runDirRender(dir string, renderConfig *renderer.WorkflowConfig) error {
	// The per-site FeedsFile is set by sitegroup; clear any inherited value.
	renderConfig.FeedsFile = ""

	summary, err := sitegroup.RenderAll(dir, renderConfig)
	if err != nil {
		return err
	}

	for i := range summary.Sites {
		s := &summary.Sites[i]
		if s.Err != nil {
			continue
		}
		logrus.Infof("  %s: %d feeds, %d items", s.Slug, s.FeedCount, s.ItemCount)
	}

	if summary.HasFailures() {
		return sitegroup.ErrPartialFailure
	}
	return nil
}
```

Add `"errors"`, `logrus`, and `sitegroup` to the imports. The local is already named `renderConfig` (a prior lint-cleanup commit renamed it — the old name `config` shadowed the imported `config` package); keep that name.

- [ ] **Step 7: Verify the wiring by hand**

```bash
make build
mkdir -p /tmp/fsdemo/opml && cd /tmp/fsdemo
printf 'https://lmorchard.com/index.rss\n' > opml/personal.txt
cat > opml/news.opml <<'XML'
<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>News</title></head>
  <body><outline text="BBC" type="rss" xmlUrl="https://feeds.bbci.co.uk/news/rss.xml" /></body>
</opml>
XML
~/devel/feedspool-go/feedspool init
~/devel/feedspool-go/feedspool fetch --feeds-dir ./opml
~/devel/feedspool-go/feedspool render --feeds-dir ./opml --output ./build
find ./build -maxdepth 2 -name index.html
```

Expected: `./build/index.html`, `./build/news/index.html`, `./build/personal/index.html`. Open `./build/index.html` and confirm both sites are listed with counts.

Also confirm the mutual-exclusion errors:

```bash
~/devel/feedspool-go/feedspool fetch --feeds-dir ./opml --filename opml/news.opml
# Expected: error "--feeds-dir cannot be combined with --format or --filename"

~/devel/feedspool-go/feedspool render --feeds-dir ./opml --feeds opml/news.opml
# Expected: error "--feeds-dir cannot be combined with --feeds"
```

Then confirm the config precedence rules:

```bash
cd /tmp/fsdemo
cat > feedspool.yaml <<'YAML'
database: ./feeds.db
feedlist:
  dir: ./opml
  filename: ./opml/news.opml
YAML
~/devel/feedspool-go/feedspool render
# Expected: error "ambiguous config: set either feedlist.dir or feedlist.filename, not both"

cat > feedspool.yaml <<'YAML'
database: ./feeds.db
feedlist:
  dir: ./opml
YAML
~/devel/feedspool-go/feedspool render --feeds opml/news.opml --output ./build-one
# Expected: single-site mode — an explicit --feeds beats the configured dir.
ls ./build-one/index.html && test ! -d ./build-one/news && echo "single-site mode confirmed"

rm feedspool.yaml
```

- [ ] **Step 8: Verify and commit**

```bash
cd ~/devel/feedspool-go
make format && make lint && make test
git add internal/config/ cmd/fetch.go cmd/render.go
git commit -m "feat: add --feeds-dir directory mode to fetch and render"
```

---

## Task 9: The `build` command

A thin fetch-then-render convenience for cron. It deliberately does **not** re-expose the full flag surface: `fetch --max-age` means "skip feeds fetched within this duration" and `render --max-age` means "the display time window" — the same name with opposite meanings. Everything beyond the four flags below comes from config.

**Files:**
- Create: `cmd/build.go`
- Modify: `.golangci.yml`

**Interfaces:**
- Consumes: `runFetch` / `runRender` internals via the shared package-level flag variables in `cmd`.
- Produces: the `feedspool build` command.

- [ ] **Step 1: Allow print statements in the new command**

`.golangci.yml` is in **v2 schema** (migrated in a prior commit), so the exclusion rules live under `linters.exclusions.rules` and put `linters:` before `path:`. Add `build` to both `cmd/(...)` regexes:

```yaml
      - linters:
          - forbidigo
        path: cmd/(build|fetch|show|purge|export|render|serve|subscribe|unsubscribe|version)\.go
      - linters:
          - nestif
        path: cmd/(build|fetch|purge|render|root)\.go
```

Do not restructure the file or add a v1-style block — `make lint` will refuse to start if the schema is wrong.

- [ ] **Step 2: Write `cmd/build.go`**

```go
package cmd

import (
	"errors"

	"github.com/sirupsen/logrus"
	"github.com/lmorchard/feedspool-go/internal/sitegroup"
	"github.com/spf13/cobra"
)

var (
	buildFeedsDir   string
	buildOutput     string
	buildClean      bool
	buildWithUnfurl bool
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Fetch feeds and render the site in one step",
	Long: `Fetch feeds and then render the static site, in that order.

Directory mode:
  feedspool build --feeds-dir ./opml      # deduped union fetch, then one site per list plus an index

Single list mode:
  feedspool build                         # uses feedlist.filename / feedlist.format from config

This is a convenience wrapper for cron. It deliberately exposes only a few
flags, because 'fetch --max-age' (skip feeds fetched recently) and
'render --max-age' (the display time window) mean opposite things. Set
everything else in feedspool.yaml and run 'feedspool fetch' / 'feedspool render'
directly when you need finer control.`,
	Args: cobra.NoArgs,
	RunE: runBuild,
}

func init() {
	buildCmd.Flags().StringVar(&buildFeedsDir, "feeds-dir", "",
		"Directory of OPML/text feed lists")
	buildCmd.Flags().StringVar(&buildOutput, "output", defaultOutputDir, "Output directory")
	buildCmd.Flags().BoolVar(&buildClean, "clean", false, "Remove output directory before building")
	buildCmd.Flags().BoolVar(&buildWithUnfurl, "with-unfurl", false,
		"Run unfurl operations in parallel with feed fetching")
	rootCmd.AddCommand(buildCmd)
}

func runBuild(cmd *cobra.Command, _ []string) error {
	// Propagate build's flags onto the fetch and render command variables, then
	// reuse their run functions so there is exactly one implementation of each
	// phase.
	fetchFeedsDir = buildFeedsDir
	fetchWithUnfurl = buildWithUnfurl

	renderFeedsDir = buildFeedsDir
	renderOutput = buildOutput
	renderClean = buildClean

	fetchErr := runFetch(cmd, nil)
	if fetchErr != nil && !errors.Is(fetchErr, sitegroup.ErrPartialFailure) {
		return fetchErr
	}
	if fetchErr != nil {
		logrus.Warn("Fetch completed with skipped feed lists; continuing to render")
	}

	if err := runRender(cmd, nil); err != nil {
		return err
	}

	return fetchErr
}
```

Note `runBuild` returns `fetchErr` at the end so a partial fetch failure still produces a non-zero exit **after** the render has run. A hard fetch error short-circuits before rendering.

- [ ] **Step 3: Verify by hand**

```bash
make build
cd /tmp/fsdemo && rm -rf build
~/devel/feedspool-go/feedspool build --feeds-dir ./opml --output ./build --clean
find ./build -maxdepth 2 -name index.html
```

Expected: the same three `index.html` files as Task 8, from one command.

Then confirm single-list mode still works:

```bash
cd /tmp/fsdemo
~/devel/feedspool-go/feedspool build --output ./build-single
# With no feeds-dir and no feedlist config, this fetches from the database and
# renders a single site into ./build-single.
ls ./build-single/index.html
```

- [ ] **Step 4: Verify and commit**

```bash
cd ~/devel/feedspool-go
make format && make lint && make test
git add cmd/build.go .golangci.yml
git commit -m "feat: add build command for fetch-then-render"
```

---

## Task 10: End-to-end integration test

**Files:**
- Modify: `integration_test.go`

**Interfaces:**
- Consumes: everything above.
- Produces: no new API.

- [ ] **Step 1: Read the existing integration test conventions**

Run: `head -60 integration_test.go` and note how it builds databases, starts `httptest` servers, and names helpers. Match that style — in particular, reuse any existing feed-XML constant rather than declaring a second one (`goconst` will flag a duplicate literal).

- [ ] **Step 2: Write the failing test**

Append to `integration_test.go`:

```go
func TestMultiSiteDirectoryBuild(t *testing.T) {
	var sharedHits int32

	shared := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&sharedHits, 1)
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(integrationFeedXML))
	}))
	defer shared.Close()

	solo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(integrationFeedXML))
	}))
	defer solo.Close()

	tmp := t.TempDir()
	listDir := filepath.Join(tmp, "opml")
	if err := os.MkdirAll(listDir, 0o750); err != nil {
		t.Fatal(err)
	}

	writeOPML := func(name, title string, urls ...string) {
		doc := `<?xml version="1.0" encoding="UTF-8"?>` + "\n<opml version=\"2.0\">\n<head><title>" +
			title + "</title></head>\n<body>\n"
		for _, u := range urls {
			doc += `<outline text="x" type="rss" xmlUrl="` + u + `" />` + "\n"
		}
		doc += "</body>\n</opml>\n"
		if err := os.WriteFile(filepath.Join(listDir, name), []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	writeOPML("alpha.opml", "Alpha", shared.URL, solo.URL)
	writeOPML("beta.opml", "Beta", shared.URL)

	dbPath := filepath.Join(tmp, "feeds.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InitSchema(); err != nil {
		t.Fatal(err)
	}
	db.Close()

	// Fetch phase: the shared feed must be requested exactly once.
	plan, err := sitegroup.PlanFetch(listDir)
	if err != nil {
		t.Fatalf("PlanFetch() error = %v", err)
	}
	if len(plan.URLs) != 2 {
		t.Fatalf("len(plan.URLs) = %d, want 2 (the shared feed must be deduped)", len(plan.URLs))
	}

	fetchDB, err := database.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	orchestrator := fetcher.NewOrchestrator(fetchDB, config.GetDefault())
	results := orchestrator.FetchFromURLs(context.Background(), plan.URLs, fetcher.FetchOptions{
		Timeout:     config.DefaultTimeout,
		MaxItems:    config.DefaultMaxItems,
		Concurrency: 2,
	})
	fetchDB.Close()

	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if got := atomic.LoadInt32(&sharedHits); got != 1 {
		t.Errorf("shared feed was requested %d times, want exactly 1", got)
	}

	// Render phase.
	outDir := filepath.Join(tmp, "build")
	summary, err := sitegroup.RenderAll(listDir, &renderer.WorkflowConfig{
		MaxAge:    "168h",
		OutputDir: outDir,
		Database:  dbPath,
	})
	if err != nil {
		t.Fatalf("RenderAll() error = %v", err)
	}
	if summary.HasFailures() {
		t.Fatalf("summary has failures: %+v", summary.Sites)
	}

	for _, path := range []string{
		filepath.Join(outDir, "index.html"),
		filepath.Join(outDir, "alpha", "index.html"),
		filepath.Join(outDir, "alpha", "index.css"),
		filepath.Join(outDir, "beta", "index.html"),
		filepath.Join(outDir, "beta", "index.css"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected %s: %v", path, err)
		}
	}

	indexHTML, err := os.ReadFile(filepath.Join(outDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`href="alpha/"`, `href="beta/"`, "Alpha", "Beta", "2 feeds", "1 feeds"} {
		if !strings.Contains(string(indexHTML), want) {
			t.Errorf("index.html does not contain %q", want)
		}
	}

	// Removing a list must prune its directory and drop it from the index.
	if err := os.Remove(filepath.Join(listDir, "beta.opml")); err != nil {
		t.Fatal(err)
	}
	if _, err := sitegroup.RenderAll(listDir, &renderer.WorkflowConfig{
		MaxAge:    "168h",
		OutputDir: outDir,
		Database:  dbPath,
	}); err != nil {
		t.Fatalf("second RenderAll() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(outDir, "beta")); !os.IsNotExist(err) {
		t.Error("beta directory survived pruning after its OPML was deleted")
	}
	indexHTML, err = os.ReadFile(filepath.Join(outDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(indexHTML), `href="beta/"`) {
		t.Error("index.html still links the pruned beta site")
	}
}
```

You will need `integrationFeedXML` — reuse the existing feed XML constant in `integration_test.go` if one exists, otherwise add:

```go
const integrationFeedXML = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
    <channel>
        <title>Integration Feed</title>
        <description>Feed for integration tests</description>
        <link>https://example.com</link>
        <item>
            <title>Integration Item</title>
            <link>https://example.com/integration-item</link>
            <description>An item</description>
            <guid>integration-item-1</guid>
        </item>
    </channel>
</rss>`
```

Imports to add as needed: `context`, `net/http`, `net/http/httptest`, `os`, `path/filepath`, `strings`, `sync/atomic`, plus `internal/config`, `internal/database`, `internal/fetcher`, `internal/renderer`, `internal/sitegroup`.

Note the item has no `<pubDate>`. Confirm how `internal/fetcher` dates such items — `clampItemDate` in `internal/fetcher/fetcher.go:156` clamps to `first_seen`, so the item should fall inside a 168h window. If the render finds zero items, add an explicit recent `<pubDate>` to the fixture.

- [ ] **Step 3: Run test to verify it fails, then passes**

Run: `go test . -run TestMultiSiteDirectoryBuild -v`

If Tasks 1–9 are complete this should pass on the first run. If it fails, the failure is a real integration gap — fix the implementation, not the test. The `sharedHits != 1` assertion in particular is the whole point of the phase split; if it reports 2, the fetch is not going through `PlanFetch`'s union.

- [ ] **Step 4: Verify and commit**

```bash
make format && make lint && make test
git add integration_test.go
git commit -m "test: add end-to-end multi-site directory build test"
```

---

## Task 11: Documentation and Docker

**Files:**
- Modify: `feedspool.yaml.example`
- Modify: `MANUAL.md`
- Modify: `README.md`
- Modify: `docker-entrypoint.sh`

**Interfaces:** none.

- [ ] **Step 1: Document `feedlist.dir` in the example config**

In `feedspool.yaml.example`, replace the feedlist block:

```yaml
# Default feed list settings
feedlist:
  format: ""    # Default format for feed lists (opml or text)
  filename: ""  # Default filename for feed lists
  dir: ""       # Directory of OPML/text feed lists; builds one site per file
                # plus a top-level index. Mutually exclusive with filename.
```

- [ ] **Step 2: Add the MANUAL.md section**

Find the `render` section in `MANUAL.md` (`grep -n '^## \|^### ' MANUAL.md` to locate it) and add a new top-level section after it. Content to write:

- **Multi-site directory mode.** What it does: one site per feed list, one deduped fetch pass, one index page.
- **Layout example** — copy the "Output Structure" tree from the spec.
- **Commands** — the three invocations (`fetch --feeds-dir`, `render --feeds-dir`, `build --feeds-dir`).
- **Naming** — filename becomes the directory slug (lowercased, non-alphanumeric runs collapsed to `-`); OPML `<head><title>` becomes the index label, falling back to the filename base; text lists always use the filename.
- **What is scanned** — `.opml` and `.txt`, non-recursive; everything else ignored.
- **Why `build` has few flags** — the `--max-age` collision; point at config for tuning.
- **Mutual exclusion** — `--feeds-dir` vs `--filename`/`--format`/`--feeds`; `feedlist.dir` vs `feedlist.filename`.
- **Pruning** — `.feedspool-sites.json`, what it does, and the guarantee that only previously-generated directories are removed.
- **Failure policy** — copy the failure table from the spec, including the non-zero exit on partial failure.

- [ ] **Step 3: Add a README quick-start line**

In `README.md`, after the existing quick-start block, add:

````markdown
Building several sites from a directory of feed lists:

```bash
mkdir opml
# drop tech.opml, comics.opml, news.txt … in there
feedspool build --feeds-dir ./opml
feedspool serve   # index at http://localhost:8080 linking one site per list
```
````

Also add a bullet to the feature-highlights list:

```markdown
- Multi-site builds from a directory of feed lists, with a shared deduped fetch
```

- [ ] **Step 4: Detect `/data/feeds.d/` in the Docker entrypoint**

In `docker-entrypoint.sh`, the config-generation block currently sets `FEED_FORMAT`/`FEED_FILENAME`. Add a directory case that takes precedence, and emit the right config key.

Replace the detection block (around lines 52-66):

```sh
    # Detect which feed list source is present and configure accordingly
    FEED_FORMAT="text"
    FEED_FILENAME="feeds.txt"
    FEED_DIR=""

    if [ -d "/data/feeds.d" ]; then
        echo "Detected feeds.d/ directory - configuring for multi-site mode"
        FEED_DIR="/data/feeds.d"
        FEED_FORMAT=""
        FEED_FILENAME=""
    elif [ -f "/data/feeds.opml" ]; then
        echo "Detected feeds.opml - configuring for OPML format"
        FEED_FORMAT="opml"
        FEED_FILENAME="feeds.opml"
    elif [ -f "/data/feeds.txt" ]; then
        echo "Detected feeds.txt - configuring for text format"
        FEED_FORMAT="text"
        FEED_FILENAME="feeds.txt"
    else
        echo "No feed list detected - will use default configuration (feeds.txt)"
    fi
```

In the generated YAML heredoc, replace the feedlist block:

```yaml
feedlist:
  format: "$FEED_FORMAT"
  filename: "$FEED_FILENAME"
  dir: "$FEED_DIR"
```

Update the confirmation echo:

```sh
    if [ -n "$FEED_DIR" ]; then
        echo "Default configuration created at /data/feedspool.yaml (multi-site dir: $FEED_DIR)"
    else
        echo "Default configuration created at /data/feedspool.yaml (format: $FEED_FORMAT, file: $FEED_FILENAME)"
    fi
```

Update the guard before the initial fetch (around line 109):

```sh
if [ -d "/data/feeds.d" ] || [ -f "/data/feeds.txt" ] || [ -f "/data/feeds.opml" ]; then
```

And add to the "no feed file found" warning list (around line 141):

```sh
    echo "  - feeds.d/    (a directory of .opml/.txt lists - builds one site each)"
```

- [ ] **Step 5: Verify the entrypoint parses**

Run: `sh -n docker-entrypoint.sh`
Expected: no output (clean parse).

- [ ] **Step 6: Verify and commit**

```bash
make format && make lint && make test
git add feedspool.yaml.example MANUAL.md README.md docker-entrypoint.sh
git commit -m "docs: document multi-site directory mode and add Docker feeds.d detection"
```

---

## Self-Review Notes

Checked against the spec:

| Spec section | Task |
|---|---|
| Output structure | 7, verified in 10 |
| CLI surface (`--feeds-dir`, `build`) | 8, 9 |
| Flag naming / mutual exclusion | 8 |
| Ambiguous-config error (`dir` + `filename`) | 8 (`Config.Validate`) |
| `--filename`/`--feeds` overrides configured `dir` | 8 |
| `internal/sitegroup` package | 4, 5, 7 |
| `feedlist.Title()` | 1 |
| `fetcher.FetchFromURLs` | 2 |
| `renderer.Result` | 3 |
| `renderer.RenderSiteIndex` | 6 |
| Discovery rules and errors | 4 |
| Fetch phase / union / remove-missing | 4, 8 |
| Render phase / sequential / clean-once | 7 |
| Index inclusion rule (`os.Stat`) | 7 (`renderIndex`) |
| Pruning + manifest + safety | 5, 7 |
| Index page template / context / assets | 6 |
| Behavior changes (`Result`, always-write-index) | 3 |
| Edge cases | 4 (discovery), 10 (shared feed) |
| Failure policy + non-zero exit | 4, 7, 8, 9 |
| Docker `feeds.d/` | 11 |
| Testing plan | 1–7, 10 |
| Documentation | 11 |
