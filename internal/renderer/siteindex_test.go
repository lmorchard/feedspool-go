package renderer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	testSiteIndexTechBlogsTitle = "Tech Blogs"
	testSiteIndexLocalNewsTitle = "Local News"
	testSiteIndexSoloFeedTitle  = "Solo Feed"
	testSiteIndexTimeWindow     = "Last 24h"
)

func TestRenderSiteIndex(t *testing.T) {
	out := t.TempDir()
	newest := time.Date(2026, 8, 22, 9, 14, 0, 0, time.UTC)

	ctx := &SiteIndexContext{
		Sites: []SiteEntry{
			{
				Slug: "tech-blogs", Title: testSiteIndexTechBlogsTitle,
				FeedCount: 42, ItemCount: 118, NewestItem: newest,
			},
			{Slug: "local-news", Title: testSiteIndexLocalNewsTitle, FeedCount: 15, ItemCount: 0},
			{Slug: "solo-feed", Title: testSiteIndexSoloFeedTitle, FeedCount: 1, ItemCount: 1, NewestItem: newest},
		},
		GeneratedAt: newest,
		TimeWindow:  testSiteIndexTimeWindow,
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
		`href="solo-feed/"`,
		testSiteIndexTechBlogsTitle,
		testSiteIndexLocalNewsTitle,
		testSiteIndexSoloFeedTitle,
		"42 feeds",
		"118 new items",
		"1 feed<",
		"1 new item<",
		"site-entry-quiet",
		`datetime="2026-08-22T09:14:00Z"`,
		testSiteIndexTimeWindow,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("index.html does not contain %q", want)
		}
	}

	// A site with exactly one feed/item must use the singular form, not "1 feeds" / "1 new items".
	for _, unwanted := range []string{"1 feeds", "1 new items"} {
		if strings.Contains(html, unwanted) {
			t.Errorf("index.html incorrectly pluralized a count of 1: found %q", unwanted)
		}
	}

	// A site with no items must not emit a bogus zero-time <time> element.
	if strings.Contains(html, "0001-01-01") {
		t.Error("index.html rendered a zero timestamp for a site with no items")
	}

	// Assets must land in the output root.
	for _, name := range []string{assetSiteIndexCSS, assetSiteIndexJS} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Errorf("expected %s in output root: %v", name, err)
		}
	}
}

// TestRenderSiteIndexCopiesOnlyThinBundle guards against RenderSiteIndex
// copying the entire feed-reader asset tree into the output root instead of
// the documented thin bundle. Before the fix, RenderSiteIndex called the same
// CopyAssets used by per-site renders, which walked every embedded asset --
// 22 files instead of the 6 the index page actually needs. That collided
// with any site directory coincidentally named "css" or "js".
func TestRenderSiteIndexCopiesOnlyThinBundle(t *testing.T) {
	out := t.TempDir()
	ctx := &SiteIndexContext{GeneratedAt: time.Now().UTC()}

	if err := RenderSiteIndex(out, "", "", ctx); err != nil {
		t.Fatalf("RenderSiteIndex() error = %v", err)
	}

	// The index's transitive CSS/JS dependencies must be present.
	for _, want := range []string{
		assetSiteIndexCSS, assetSiteIndexJS,
		assetCSSVariables, assetCSSBase, assetCSSSiteIndex, assetJSTimeFormatter,
	} {
		if _, err := os.Stat(filepath.Join(out, want)); err != nil {
			t.Errorf("expected %s in site index output: %v", want, err)
		}
	}

	// The full feed-reader bundle must NOT be copied into the index root.
	for _, unwanted := range []string{
		testAssetIndexCSS, testAssetIndexJS,
		filepath.Join("css", "feed.css"),
		filepath.Join("css", "item.css"),
		filepath.Join("css", "layout.css"),
		filepath.Join("css", "lightbox.css"),
		filepath.Join("css", "view-modes.css"),
		filepath.Join("js", "content-isolation-iframe.js"),
		filepath.Join("js", "feed-navigator.js"),
		filepath.Join("js", "layout-controller.js"),
		filepath.Join("js", "lazy-image-loader.js"),
		filepath.Join("js", "lightbox-overlay.js"),
		filepath.Join("js", "link-loader.js"),
		filepath.Join("js", "utils.js"),
	} {
		if _, err := os.Stat(filepath.Join(out, unwanted)); err == nil {
			t.Errorf("site index output contains %s, which belongs only to the feed-reader bundle", unwanted)
		}
	}
}
