package sitegroup

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/renderer"
)

const (
	techOPML   = "tech.opml"
	comicsOPML = "comics.opml"
	maxAge24h  = "24h"
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
		filepath.Join(out, techSlug, "index.html"),
		filepath.Join(out, ManifestName),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected %s to exist: %v", path, err)
		}
	}

	// Existence alone doesn't prove the index actually links each site: an
	// empty "No feed lists found." page would also pass the checks above.
	indexHTML, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatalf("failed to read index.html: %v", err)
	}
	for _, want := range []string{`href="` + comicsSlug + `/"`, `href="` + techSlug + `/"`} {
		if !strings.Contains(string(indexHTML), want) {
			t.Errorf("index.html does not contain %q", want)
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

// TestRenderAllCleanDoesNotDestroyOnBadDirectory guards against a data-loss
// bug: --clean must never remove the output root before the input directory
// is known to be valid. A typo'd --dir with --clean must fail without
// touching anything that was already published there.
func TestRenderAllCleanDoesNotDestroyOnBadDirectory(t *testing.T) {
	dir := writeDir(t, map[string]string{notesMD: "no feed lists here"})
	out := t.TempDir()

	survivor := filepath.Join(out, "index.html")
	if err := os.WriteFile(survivor, []byte("previously published"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := RenderAll(dir, &renderer.WorkflowConfig{
		MaxAge:    maxAge24h,
		OutputDir: out,
		Database:  newTestDB(t),
		Clean:     true,
	})
	if err == nil {
		t.Fatal("RenderAll() error = nil, want an error for a directory with no feed lists")
	}
	if _, statErr := os.Stat(survivor); statErr != nil {
		t.Errorf("--clean destroyed the output root before validating the input directory: %v", statErr)
	}
}

func TestRenderAllCleanRemovesOutputRootOnce(t *testing.T) {
	dbPath := newTestDB(t)
	out := filepath.Join(t.TempDir(), "build")
	dir := writeDir(t, map[string]string{
		techOPML:   opmlWith("Tech", feedA),
		comicsOPML: opmlWith("Comics", feedB),
	})

	if err := os.MkdirAll(out, 0o750); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(out, "stale.html")
	if err := os.WriteFile(stale, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A marker inside a site's own output subdirectory, not just at the
	// output root: this is what a per-site clean (rather than a single
	// root-level clean) would additionally have the opportunity to remove.
	siteMarkerDir := filepath.Join(out, comicsSlug)
	if err := os.MkdirAll(siteMarkerDir, 0o750); err != nil {
		t.Fatal(err)
	}
	siteMarker := filepath.Join(siteMarkerDir, "leftover.txt")
	if err := os.WriteFile(siteMarker, []byte("old"), 0o600); err != nil {
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
	if _, err := os.Stat(siteMarker); !os.IsNotExist(err) {
		t.Error("--clean did not remove a leftover file inside a site's own output subdirectory")
	}
	if _, err := os.Stat(filepath.Join(out, techSlug, "index.html")); err != nil {
		t.Errorf("site was not built after clean: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, comicsSlug, "index.html")); err != nil {
		t.Errorf("site was not built after clean: %v", err)
	}
}

// TestRenderAllReportsPerSiteFailuresAndKeepsStaleIndexLinks exercises the
// per-site failure path (nothing currently makes ExecuteWorkflow fail except
// an uninitialized database) and, in the same run, the index's stale-link and
// never-built-is-omitted policy: a first pass publishes tech successfully; a
// second pass adds a never-built comics site and points at an uninitialized
// database, so every site fails to render this time.
func TestRenderAllReportsPerSiteFailuresAndKeepsStaleIndexLinks(t *testing.T) {
	dir := writeDir(t, map[string]string{techOPML: opmlWith("Tech", feedA)})
	out := filepath.Join(t.TempDir(), "build")

	// First pass succeeds and publishes tech.
	if _, err := RenderAll(dir, &renderer.WorkflowConfig{
		MaxAge:    maxAge24h,
		OutputDir: out,
		Database:  newTestDB(t),
	}); err != nil {
		t.Fatalf("first RenderAll() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, techSlug, "index.html")); err != nil {
		t.Fatalf("tech was not built in the first pass: %v", err)
	}

	// Add a second site that has never built successfully, then re-render
	// against a database that has never been initialized, so every site
	// fails this pass with "database not initialized".
	if err := os.WriteFile(filepath.Join(dir, comicsOPML), []byte(opmlWith("Comics", feedB)), 0o600); err != nil {
		t.Fatal(err)
	}
	badDB := filepath.Join(t.TempDir(), "uninitialized.db")

	summary, err := RenderAll(dir, &renderer.WorkflowConfig{
		MaxAge:    maxAge24h,
		OutputDir: out,
		Database:  badDB,
	})
	if err != nil {
		t.Fatalf("second RenderAll() error = %v, want nil: per-site failures must not be fatal", err)
	}
	if !summary.HasFailures() {
		t.Fatal("HasFailures() = false, want true when every site fails to render")
	}
	if len(summary.Sites) != 2 {
		t.Fatalf("len(Sites) = %d, want 2", len(summary.Sites))
	}
	for i := range summary.Sites {
		if summary.Sites[i].Err == nil {
			t.Errorf("Sites[%d].Err = nil, want the uninitialized-database error", i)
		}
	}
	if _, err := os.Stat(filepath.Join(out, ManifestName)); err != nil {
		t.Errorf("manifest was not written despite per-site failures: %v", err)
	}

	// RenderSiteIndex needs no database, so the index must still render, and
	// per the documented policy: previously-built sites stay linked with
	// stale content, and sites that never built are omitted entirely.
	indexHTML, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatalf("failed to read index.html: %v", err)
	}
	index := string(indexHTML)
	if !strings.Contains(index, `href="`+techSlug+`/"`) {
		t.Errorf("index.html does not link the previously-built %s site; stale content should beat a dead link", techSlug)
	}
	if strings.Contains(index, `href="`+comicsSlug+`/"`) {
		t.Errorf("index.html links %s, which never built successfully", comicsSlug)
	}
}

// TestRenderAllIndexTimeWindowMatchesSitePages guards against the index page
// silently disagreeing with what a per-site page shows for the same explicit
// --start/--end window: both must go through the same parsed times and the
// same formatting function, rather than the index echoing the raw,
// unparsed input strings.
func TestRenderAllIndexTimeWindowMatchesSitePages(t *testing.T) {
	dir := writeDir(t, map[string]string{techOPML: opmlWith("Tech", feedA)})
	out := filepath.Join(t.TempDir(), "build")

	start := "2024-01-01T00:00:00Z"
	end := "2024-01-02T00:00:00Z"
	base := &renderer.WorkflowConfig{
		Start:     start,
		End:       end,
		OutputDir: out,
		Database:  newTestDB(t),
	}

	if _, err := RenderAll(dir, base); err != nil {
		t.Fatalf("RenderAll() error = %v", err)
	}

	startTime, endTime, err := database.ParseTimeWindow(base.MaxAge, base.Start, base.End)
	if err != nil {
		t.Fatalf("database.ParseTimeWindow() error = %v", err)
	}
	want := renderer.FormatTimeWindow(startTime, endTime, base.MaxAge)

	indexHTML, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatalf("failed to read index.html: %v", err)
	}
	if !strings.Contains(string(indexHTML), want) {
		t.Errorf("index.html does not contain the formatted time window %q; got:\n%s", want, indexHTML)
	}

	// Guard against the previous bug: the raw, unparsed start/end strings
	// must not appear verbatim, since the per-site pages never show that
	// dangling, unformatted form.
	rawForm := "From " + start + " to " + end
	if strings.Contains(string(indexHTML), rawForm) {
		t.Errorf("index.html shows the raw --start/--end strings %q instead of a parsed, formatted window", rawForm)
	}
}
