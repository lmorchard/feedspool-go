package renderer

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
)

const testFeedURL = "https://example.com/feed.xml"

// testAssetIndexCSS and testAssetIndexJS name the feed-reader bundle's entry
// points, shared with siteindex_test.go's assertion that those files must
// not appear in the site-index bundle.
const (
	testAssetIndexCSS = "index.css"
	testAssetIndexJS  = "index.js"
)

// testFormatOPML and testTitleTechBlogs are shared across the site-title
// tests below; goconst flags the repeated literals otherwise.
const (
	testFormatOPML         = "opml"
	testTitleTechBlogs     = "Tech Blogs"
	testH1TechBlogs        = "<h1>Tech Blogs</h1>"
	testTitleTechBlogsHTML = "<title>Tech Blogs</title>"
)

// newTestWorkflow builds a database with one feed and two items, and returns a
// WorkflowConfig pointing at it with a fresh output directory, along with the
// published date of the newest of the two items it inserted (zero if
// withFeed is false). The caller can assert Result.NewestItem against that
// value exactly, rather than merely checking it is non-zero.
func newTestWorkflow(t *testing.T, withFeed bool) (*WorkflowConfig, time.Time) {
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

	var newestItem time.Time
	if withFeed {
		now := time.Now().UTC()
		if err := db.UpsertFeed(&database.Feed{
			URL:                 testFeedURL,
			Title:               "Example",
			LastFetchTime:       now,
			LastSuccessfulFetch: now,
			LatestItemDate:      sql.NullTime{Time: now, Valid: true},
		}); err != nil {
			t.Fatal(err)
		}
		for i, offset := range []time.Duration{-2 * time.Hour, -1 * time.Hour} {
			publishedDate := now.Add(offset)
			if err := db.UpsertItem(&database.Item{
				FeedURL:       testFeedURL,
				GUID:          string(rune('a' + i)),
				Title:         "Item",
				Link:          "https://example.com/item",
				PublishedDate: publishedDate,
			}); err != nil {
				t.Fatal(err)
			}
			if publishedDate.After(newestItem) {
				newestItem = publishedDate
			}
		}
	}
	db.Close()

	return &WorkflowConfig{
		MaxAge:    "24h",
		OutputDir: filepath.Join(tmpDir, "build"),
		Database:  dbPath,
	}, newestItem
}

func TestExecuteWorkflowResult(t *testing.T) {
	cfg, wantNewestItem := newTestWorkflow(t, true)

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
	// The database round-trips a time.Time through SQLite with full
	// nanosecond precision and normalizes it to UTC, so exact equality (via
	// Equal, not ==, since the round-tripped value may carry a different
	// *time.Location representing the same instant) is reliable here rather
	// than needing a tolerance window.
	if !result.NewestItem.Equal(wantNewestItem) {
		t.Errorf("NewestItem = %v, want %v (the newest of the two inserted items)", result.NewestItem, wantNewestItem)
	}
}

func TestExecuteWorkflowUsesFirstSeenForScrapedItemDates(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "scrape.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InitSchema(); err != nil {
		t.Fatal(err)
	}
	firstSeen := time.Date(2026, time.August, 25, 20, 30, 0, 0, time.UTC)
	const scrapedItemURL = "https://example.com/scraped"
	if err := db.UpsertFeed(&database.Feed{
		URL:            testFeedURL,
		Title:          "Scraped Feed",
		Type:           database.FeedTypeScrape,
		ScrapeSelector: ".post",
		LatestItemDate: sql.NullTime{Time: firstSeen, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertItem(&database.Item{
		FeedURL:   testFeedURL,
		GUID:      scrapedItemURL,
		Title:     "Scraped item",
		Link:      scrapedItemURL,
		FirstSeen: sql.NullTime{Time: firstSeen, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	cfg := &WorkflowConfig{
		Start:     firstSeen.Add(-time.Minute).Format(time.RFC3339),
		End:       firstSeen.Add(time.Minute).Format(time.RFC3339),
		OutputDir: filepath.Join(tmpDir, "build"),
		Database:  dbPath,
	}
	result, err := ExecuteWorkflow(cfg)
	if err != nil {
		t.Fatalf("ExecuteWorkflow() error = %v", err)
	}
	if !result.NewestItem.Equal(firstSeen) {
		t.Errorf("NewestItem = %v, want first_seen %v", result.NewestItem, firstSeen)
	}
	page, err := os.ReadFile(filepath.Join(cfg.OutputDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), firstSeen.Format("Jan 2, 2006 15:04 UTC")) {
		t.Errorf("rendered page does not contain first_seen date: %s", page)
	}
}

func TestExecuteWorkflowWritesIndexWhenEmpty(t *testing.T) {
	cfg, _ := newTestWorkflow(t, false)

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

// TestExecuteWorkflowAssetBundleExcludesSiteIndexAssets guards spec Goal 4:
// a single-list render's asset bundle must be byte-for-byte what it was
// before multi-site directory mode existed. CopyAssets previously walked the
// entire embedded assets tree indiscriminately, so once site-index.css,
// site-index.js, and css/site-index.css were added for the multi-site index
// page, every single-list render started emitting them too.
func TestExecuteWorkflowAssetBundleExcludesSiteIndexAssets(t *testing.T) {
	cfg, _ := newTestWorkflow(t, true)

	if _, err := ExecuteWorkflow(cfg); err != nil {
		t.Fatalf("ExecuteWorkflow() error = %v", err)
	}

	for _, unwanted := range []string{assetSiteIndexCSS, assetSiteIndexJS, assetCSSSiteIndex} {
		if _, err := os.Stat(filepath.Join(cfg.OutputDir, unwanted)); err == nil {
			t.Errorf("single-list render output contains %s, which belongs only to the multi-site index bundle", unwanted)
		}
	}

	// The feed-reader bundle itself must still be present.
	for _, want := range []string{testAssetIndexCSS, testAssetIndexJS, assetCSSBase, assetJSTimeFormatter} {
		if _, err := os.Stat(filepath.Join(cfg.OutputDir, want)); err != nil {
			t.Errorf("expected %s in single-list render output: %v", want, err)
		}
	}
}

// TestExecuteWorkflowRendersGeneratedAt guards SiteChrome's embedding: the
// three context structs get GeneratedAt via Go's promotion of an embedded
// struct's fields rather than declaring it themselves, and index.html
// references it bare in its footer. Promotion has to resolve two levels deep
// there, because renderIndexFile wraps the context in a local IndexContext
// that embeds *TemplateContext.
func TestExecuteWorkflowRendersGeneratedAt(t *testing.T) {
	cfg, _ := newTestWorkflow(t, true)

	if _, err := ExecuteWorkflow(cfg); err != nil {
		t.Fatalf("ExecuteWorkflow() error = %v", err)
	}

	indexHTML, err := os.ReadFile(filepath.Join(cfg.OutputDir, "index.html"))
	if err != nil {
		t.Fatalf("failed to read index.html: %v", err)
	}
	// The footer renders {{.GeneratedAt.Format "2006-01-02 15:04:05 UTC"}}.
	// A zero GeneratedAt would render year 0001, so asserting the current
	// year proves the real value arrived rather than a zero value.
	want := "Generated by feedspool at " + time.Now().UTC().Format("2006")
	if !strings.Contains(string(indexHTML), want) {
		t.Errorf("index.html footer does not contain %q; got:\n%s", want, indexHTML)
	}
}

// TestExecuteWorkflowCustomTemplateResolvesChromeFields renders through a
// user-supplied template directory rather than the embedded one, standing in
// for a template extracted with `init --extract-templates`. Such a template
// references {{.TimeWindow}} and {{.GeneratedAt}} bare; after those fields
// move into an embedded SiteChrome they resolve only via Go's field
// promotion. A template that stopped resolving them would render empty
// strings rather than erroring, so assert on the output.
func TestExecuteWorkflowCustomTemplateResolvesChromeFields(t *testing.T) {
	templatesDir := t.TempDir()
	custom := "<html><body><p>window={{.TimeWindow}}</p>" +
		`<p>gen={{.GeneratedAt.Format "2006"}}</p></body></html>` + "\n"
	if err := os.WriteFile(filepath.Join(templatesDir, "index.html"), []byte(custom), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, _ := newTestWorkflow(t, true)
	cfg.TemplatesDir = templatesDir

	if _, err := ExecuteWorkflow(cfg); err != nil {
		t.Fatalf("ExecuteWorkflow() error = %v", err)
	}

	indexHTML, err := os.ReadFile(filepath.Join(cfg.OutputDir, "index.html"))
	if err != nil {
		t.Fatalf("failed to read index.html: %v", err)
	}
	got := string(indexHTML)
	if strings.Contains(got, "window=</p>") {
		t.Errorf("custom template rendered an empty TimeWindow; got:\n%s", got)
	}
	if !strings.Contains(got, "gen="+time.Now().UTC().Format("2006")) {
		t.Errorf("custom template rendered an empty GeneratedAt; got:\n%s", got)
	}
}

// writeOPML writes an OPML file containing one feed into dir and returns its
// path. A blank title omits the <title> element entirely, which is the
// "untitled OPML" case the filename fallback exists for.
func writeOPML(t *testing.T, dir, name, title string) string {
	t.Helper()

	head := "<head></head>"
	if title != "" {
		head = "<head><title>" + title + "</title></head>"
	}
	doc := `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<opml version="2.0">` + "\n" + head + "\n<body>\n" +
		`<outline text="x" type="rss" xmlUrl="` + testFeedURL + `" />` + "\n" +
		"</body>\n</opml>\n"

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadFeedURLsDerivesTitle(t *testing.T) {
	dir := t.TempDir()

	txtPath := filepath.Join(dir, "scratch.txt")
	if err := os.WriteFile(txtPath, []byte(testFeedURL+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		file      string
		format    string
		wantTitle string
	}{
		{
			name:      "opml head title wins",
			file:      writeOPML(t, dir, "tech.opml", testTitleTechBlogs),
			format:    testFormatOPML,
			wantTitle: testTitleTechBlogs,
		},
		{
			name:      "untitled opml falls back to filename base",
			file:      writeOPML(t, dir, "comics.opml", ""),
			format:    testFormatOPML,
			wantTitle: "comics",
		},
		{
			name:      "whitespace-only opml title falls back to filename base",
			file:      writeOPML(t, dir, "blank.opml", "   "),
			format:    testFormatOPML,
			wantTitle: "blank",
		},
		{
			name:      "text list falls back to filename base",
			file:      txtPath,
			format:    "text",
			wantTitle: "scratch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			urls, title, err := loadFeedURLs(tt.file, tt.format)
			if err != nil {
				t.Fatalf("loadFeedURLs() error = %v", err)
			}
			if title != tt.wantTitle {
				t.Errorf("title = %q, want %q", title, tt.wantTitle)
			}
			if len(urls) != 1 {
				t.Errorf("len(urls) = %d, want 1", len(urls))
			}
		})
	}
}

func TestLoadFeedURLsNoFileHasNoTitle(t *testing.T) {
	urls, title, err := loadFeedURLs("", "")
	if err != nil {
		t.Fatalf("loadFeedURLs() error = %v", err)
	}
	if title != "" {
		t.Errorf("title = %q, want empty for database mode", title)
	}
	if urls != nil {
		t.Errorf("urls = %v, want nil", urls)
	}
}

func TestExecuteWorkflowIndexUsesFeedListTitle(t *testing.T) {
	cfg, _ := newTestWorkflow(t, true)
	cfg.FeedsFile = writeOPML(t, t.TempDir(), "tech.opml", testTitleTechBlogs)
	cfg.Format = testFormatOPML

	if _, err := ExecuteWorkflow(cfg); err != nil {
		t.Fatalf("ExecuteWorkflow() error = %v", err)
	}

	indexHTML, err := os.ReadFile(filepath.Join(cfg.OutputDir, "index.html"))
	if err != nil {
		t.Fatalf("failed to read index.html: %v", err)
	}
	for _, want := range []string{testTitleTechBlogsHTML, testH1TechBlogs} {
		if !strings.Contains(string(indexHTML), want) {
			t.Errorf("index.html does not contain %q", want)
		}
	}
	if strings.Contains(string(indexHTML), ">"+DefaultSiteTitle+"<") {
		t.Errorf("index.html still contains the default title %q", DefaultSiteTitle)
	}
}

func TestExecuteWorkflowSiteTitleOverrideWins(t *testing.T) {
	cfg, _ := newTestWorkflow(t, true)
	cfg.FeedsFile = writeOPML(t, t.TempDir(), "tech.opml", testTitleTechBlogs)
	cfg.Format = testFormatOPML
	cfg.SiteTitle = "Explicit Override"

	if _, err := ExecuteWorkflow(cfg); err != nil {
		t.Fatalf("ExecuteWorkflow() error = %v", err)
	}

	indexHTML, err := os.ReadFile(filepath.Join(cfg.OutputDir, "index.html"))
	if err != nil {
		t.Fatalf("failed to read index.html: %v", err)
	}
	if !strings.Contains(string(indexHTML), "<h1>Explicit Override</h1>") {
		t.Errorf("index.html does not use the SiteTitle override")
	}
	if strings.Contains(string(indexHTML), testTitleTechBlogs) {
		t.Errorf("index.html used the feed list title despite an explicit override")
	}
}

func TestExecuteWorkflowDatabaseModeUsesDefaultTitle(t *testing.T) {
	cfg, _ := newTestWorkflow(t, true) // No FeedsFile: database mode.

	if _, err := ExecuteWorkflow(cfg); err != nil {
		t.Fatalf("ExecuteWorkflow() error = %v", err)
	}

	indexHTML, err := os.ReadFile(filepath.Join(cfg.OutputDir, "index.html"))
	if err != nil {
		t.Fatalf("failed to read index.html: %v", err)
	}
	if !strings.Contains(string(indexHTML), "<h1>"+DefaultSiteTitle+"</h1>") {
		t.Errorf("index.html does not fall back to %q with no feed list", DefaultSiteTitle)
	}
}

func TestExecuteWorkflowEscapesTitleMetacharacters(t *testing.T) {
	cfg, _ := newTestWorkflow(t, true)
	cfg.FeedsFile = writeOPML(t, t.TempDir(), "evil.opml", "A &amp; B &lt;script&gt;")
	cfg.Format = testFormatOPML

	if _, err := ExecuteWorkflow(cfg); err != nil {
		t.Fatalf("ExecuteWorkflow() error = %v", err)
	}

	indexHTML, err := os.ReadFile(filepath.Join(cfg.OutputDir, "index.html"))
	if err != nil {
		t.Fatalf("failed to read index.html: %v", err)
	}
	// The OPML parser decodes the entities to `A & B <script>`; html/template
	// must re-escape them rather than emitting a live tag.
	if strings.Contains(string(indexHTML), "<script>") {
		t.Errorf("index.html contains an unescaped <script> from the feed list title")
	}
}

// feedPagePath returns the path of the single feed page written under
// outputDir. newTestWorkflow inserts exactly one feed, so exactly one page
// exists; finding it by glob avoids duplicating generateFeedID's hashing.
func feedPagePath(t *testing.T, outputDir string) string {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(outputDir, "feeds", "*.html"))
	if err != nil {
		t.Fatal(err)
	}
	var pages []string
	for _, m := range matches {
		// Skip pagination fragments; only per-feed pages are wanted here.
		if !strings.HasPrefix(filepath.Base(m), "page-") {
			pages = append(pages, m)
		}
	}
	if len(pages) != 1 {
		t.Fatalf("found %d feed pages under %s, want 1: %v", len(pages), outputDir, pages)
	}
	return pages[0]
}

func TestFeedPageUsesFeedListTitle(t *testing.T) {
	cfg, _ := newTestWorkflow(t, true)
	cfg.FeedsFile = writeOPML(t, t.TempDir(), "tech.opml", testTitleTechBlogs)
	cfg.Format = testFormatOPML

	if _, err := ExecuteWorkflow(cfg); err != nil {
		t.Fatalf("ExecuteWorkflow() error = %v", err)
	}

	page, err := os.ReadFile(feedPagePath(t, cfg.OutputDir))
	if err != nil {
		t.Fatalf("failed to read feed page: %v", err)
	}
	// newTestWorkflow's feed is titled "Example".
	for _, want := range []string{"<title>Example - Tech Blogs</title>", testH1TechBlogs} {
		if !strings.Contains(string(page), want) {
			t.Errorf("feed page does not contain %q", want)
		}
	}
	if strings.Contains(string(page), "Feed Reader") {
		t.Errorf("feed page still contains the generic \"Feed Reader\" suffix")
	}
}

func TestFeedPageDatabaseModeUsesDefaultTitle(t *testing.T) {
	cfg, _ := newTestWorkflow(t, true) // No FeedsFile: database mode.

	if _, err := ExecuteWorkflow(cfg); err != nil {
		t.Fatalf("ExecuteWorkflow() error = %v", err)
	}

	page, err := os.ReadFile(feedPagePath(t, cfg.OutputDir))
	if err != nil {
		t.Fatalf("failed to read feed page: %v", err)
	}
	for _, want := range []string{
		"<title>Example - " + DefaultSiteTitle + "</title>",
		"<h1>" + DefaultSiteTitle + "</h1>",
	} {
		if !strings.Contains(string(page), want) {
			t.Errorf("feed page does not contain %q", want)
		}
	}
}
