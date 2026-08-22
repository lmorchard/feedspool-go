package sitegroup

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/renderer"
)

const (
	techOPML       = "tech.opml"
	comicsOPML     = "comics.opml"
	goodOPML       = "good.opml"
	maxAge24h      = "24h"
	badOPMLContent = "<opml><head><title>unclosed"
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
			LatestItemDate:      sql.NullTime{Time: now, Valid: true},
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
		techOPML:   opmlWith("Tech", feedA),
		comicsOPML: opmlWith("Comics", feedA, feedB),
	})
	out := filepath.Join(t.TempDir(), "build")

	summary, err := RenderAll(dir, &renderer.WorkflowConfig{
		MaxAge:    maxAge24h,
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
	if summary.Sites[0].Slug != comicsSlug {
		t.Errorf("Sites[0].Slug = %q, want comics slug", summary.Sites[0].Slug)
	}
	if summary.Sites[0].FeedCount != 2 {
		t.Errorf("comics FeedCount = %d, want 2", summary.Sites[0].FeedCount)
	}
	if summary.Sites[1].FeedCount != 1 {
		t.Errorf("tech FeedCount = %d, want 1", summary.Sites[1].FeedCount)
	}

	for _, path := range []string{
		filepath.Join(out, "index.html"),
		filepath.Join(out, comicsSlug, "index.html"),
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
		techOPML:   opmlWith("Tech", feedA),
		comicsOPML: opmlWith("Comics", feedB),
	})
	base := &renderer.WorkflowConfig{MaxAge: maxAge24h, OutputDir: out, Database: dbPath}

	if _, err := RenderAll(dir, base); err != nil {
		t.Fatalf("first RenderAll() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, comicsSlug)); err != nil {
		t.Fatalf("comics was not built: %v", err)
	}

	if err := os.Remove(filepath.Join(dir, comicsOPML)); err != nil {
		t.Fatal(err)
	}

	summary, err := RenderAll(dir, base)
	if err != nil {
		t.Fatalf("second RenderAll() error = %v", err)
	}
	if len(summary.Removed) != 1 || summary.Removed[0] != comicsSlug {
		t.Errorf("Removed = %v, want [comics]", summary.Removed)
	}
	if _, err := os.Stat(filepath.Join(out, comicsSlug)); !os.IsNotExist(err) {
		t.Error("comics directory survived pruning")
	}
}

func TestRenderAllReportsSkippedFiles(t *testing.T) {
	dir := writeDir(t, map[string]string{
		goodOPML: opmlWith("Good", feedA),
		badOPML:  badOPMLContent,
	})
	out := filepath.Join(t.TempDir(), "build")

	summary, err := RenderAll(dir, &renderer.WorkflowConfig{
		MaxAge:    maxAge24h,
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
	dir := writeDir(t, map[string]string{techOPML: opmlWith("Tech", feedA)})

	if err := os.MkdirAll(out, 0o750); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(out, "stale.html")
	if err := os.WriteFile(stale, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := RenderAll(dir, &renderer.WorkflowConfig{
		MaxAge:    maxAge24h,
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
