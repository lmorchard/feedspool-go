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
		testSiteIndexTechBlogsTitle,
		testSiteIndexLocalNewsTitle,
		"42 feeds",
		"118 new items",
		"site-entry-quiet",
		`datetime="2026-08-22T09:14:00Z"`,
		testSiteIndexTimeWindow,
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
