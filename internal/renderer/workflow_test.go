package renderer

import (
	"database/sql"
	"os"
	"path/filepath"
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
