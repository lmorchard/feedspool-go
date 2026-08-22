package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/fetcher"
	"github.com/lmorchard/feedspool-go/internal/renderer"
	"github.com/lmorchard/feedspool-go/internal/sitegroup"
)

const integrationTestFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
    <channel>
        <title>Integration Test Feed</title>
        <description>A test RSS feed for integration testing</description>
        <link>https://example.com</link>
        <item>
            <title>Integration Test Item 1</title>
            <link>https://example.com/item1</link>
            <description>First integration test item</description>
            <pubDate>Mon, 01 Jan 2024 12:00:00 GMT</pubDate>
            <guid>integration-item-1</guid>
        </item>
        <item>
            <title>Integration Test Item 2</title>
            <link>https://example.com/item2</link>
            <description>Second integration test item</description>
            <pubDate>Mon, 01 Jan 2024 13:00:00 GMT</pubDate>
            <guid>integration-item-2</guid>
        </item>
    </channel>
</rss>`

func TestIntegrationEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Build the feedspool binary
	binaryPath := buildBinary(t)
	defer os.Remove(binaryPath)

	// Create temporary directory for test
	testDir := t.TempDir()

	dbPath := filepath.Join(testDir, "feeds.db")

	// Create test HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Header().Set("ETag", "test-etag-123")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(integrationTestFeed))
	}))
	defer server.Close()

	// Create OPML file
	opmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
    <head>
        <title>Integration Test OPML</title>
    </head>
    <body>
        <outline text="Integration Test Feed" type="rss" xmlUrl="` + server.URL + `" />
    </body>
</opml>`

	opmlPath := filepath.Join(testDir, "feeds.opml")
	err := os.WriteFile(opmlPath, []byte(opmlContent), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	// Test complete workflow
	t.Run("init_database", func(t *testing.T) {
		output, err := runCommand(binaryPath, "--database", dbPath, "init")
		if err != nil {
			t.Errorf("Init failed: %v, output: %s", err, output)
		}

		if !strings.Contains(output, "initialized") {
			t.Errorf("Init output should mention initialization, got: %s", output)
		}

		// Verify database file exists
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			t.Error("Database file should exist after init")
		}
	})

	t.Run("fetch_from_opml", func(t *testing.T) {
		output, err := runCommand(binaryPath, "--database", dbPath, "fetch", "--format", "opml", "--filename", opmlPath)
		if err != nil {
			t.Errorf("Fetch from OPML failed: %v, output: %s", err, output)
		}

		if !strings.Contains(output, "Found") && !strings.Contains(output, "Summary") {
			t.Errorf("Fetch output should show results, got: %s", output)
		}
	})

	t.Run("show_feed_contents", func(t *testing.T) {
		output, err := runCommand(binaryPath, "--database", dbPath, "show", server.URL)
		if err != nil {
			t.Errorf("Show failed: %v, output: %s", err, output)
		}

		if !strings.Contains(output, "Integration Test Item") {
			t.Errorf("Show output should contain items, got: %s", output)
		}

		if !strings.Contains(output, "Integration Test Item 1") {
			t.Errorf("Show output should contain first item, got: %s", output)
		}

		if !strings.Contains(output, "Integration Test Item 2") {
			t.Errorf("Show output should contain second item, got: %s", output)
		}
	})

	t.Run("show_json_output", func(t *testing.T) {
		output, err := runCommand(binaryPath, "--database", dbPath, "--json", "show", server.URL)
		if err != nil {
			t.Errorf("Show JSON failed: %v, output: %s", err, output)
		}

		// Check for feed-level fields
		if !strings.Contains(output, `"Title"`) {
			t.Errorf("JSON output should contain feed Title field, got: %s", output)
		}

		if !strings.Contains(output, `"URL"`) {
			t.Errorf("JSON output should contain feed URL field, got: %s", output)
		}

		// Check for Items array and item fields
		if !strings.Contains(output, `"Items"`) {
			t.Errorf("JSON output should contain Items array, got: %s", output)
		}

		if !strings.Contains(output, `"GUID"`) {
			t.Errorf("JSON output should contain item GUID field, got: %s", output)
		}

		// Check that FeedJSON and ItemJSON are objects, not base64 strings
		if strings.Contains(output, `"FeedJSON":"eyJ`) || strings.Contains(output, `"ItemJSON":"eyJ`) {
			t.Errorf("JSON output should not contain base64-encoded JSON fields, got: %s", output)
		}
	})

	t.Run("fetch_single_feed", func(t *testing.T) {
		// Clear database and reinit to test single fetch
		os.Remove(dbPath)
		runCommand(binaryPath, "--database", dbPath, "init")

		output, err := runCommand(binaryPath, "--database", dbPath, "fetch", server.URL)
		if err != nil {
			t.Errorf("Fetch failed: %v, output: %s", err, output)
		}

		if !strings.Contains(output, "fetched") && !strings.Contains(output, "Items:") {
			t.Errorf("Fetch output should show results, got: %s", output)
		}

		// Verify we can show the fetched feed
		showOutput, err := runCommand(binaryPath, "--database", dbPath, "show", server.URL)
		if err != nil {
			t.Errorf("Show after fetch failed: %v", err)
		}

		if !strings.Contains(showOutput, "Integration Test Item") {
			t.Errorf("Show should work after fetch, got: %s", showOutput)
		}
	})

	t.Run("purge_dry_run", func(t *testing.T) {
		output, err := runCommand(binaryPath, "--database", dbPath, "purge", "--dry-run")
		if err != nil {
			t.Errorf("Purge dry run failed: %v, output: %s", err, output)
		}

		if !strings.Contains(output, "Dry run") && !strings.Contains(output, "would delete") {
			t.Errorf("Purge dry run output should mention dry run, got: %s", output)
		}
	})

	t.Run("version_command", func(t *testing.T) {
		output, err := runCommand(binaryPath, "version")
		if err != nil {
			t.Errorf("Version failed: %v, output: %s", err, output)
		}

		if !strings.Contains(output, "feedspool") {
			t.Errorf("Version output should contain program name, got: %s", output)
		}
	})
}

func TestIntegrationCaching(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Build the feedspool binary
	binaryPath := buildBinary(t)
	defer os.Remove(binaryPath)

	// Create temporary directory
	testDir := t.TempDir()

	dbPath := filepath.Join(testDir, "feeds.db")

	var requestCount int64
	// Create test HTTP server that tracks requests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt64(&requestCount, 1)

		// Check for conditional headers on second request
		if count > 1 {
			if r.Header.Get("If-None-Match") == "test-etag-123" {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}

		w.Header().Set("Content-Type", "application/rss+xml")
		w.Header().Set("ETag", "test-etag-123")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(integrationTestFeed))
	}))
	defer server.Close()

	// Initialize database
	runCommand(binaryPath, "--database", dbPath, "init")

	// First fetch - should get full response
	output1, err := runCommand(binaryPath, "--database", dbPath, "fetch", server.URL)
	if err != nil {
		t.Errorf("First fetch failed: %v, output: %s", err, output1)
	}

	if count := atomic.LoadInt64(&requestCount); count != 1 {
		t.Errorf("Expected 1 request after first fetch, got %d", count)
	}

	// Second fetch - should use cache (304 response)
	output2, err := runCommand(binaryPath, "--database", dbPath, "fetch", server.URL)
	if err != nil {
		t.Errorf("Second fetch failed: %v, output: %s", err, output2)
	}

	if count := atomic.LoadInt64(&requestCount); count != 2 {
		t.Errorf("Expected 2 requests after second fetch, got %d", count)
	}

	// Third fetch with force flag - should bypass cache
	output3, err := runCommand(binaryPath, "--database", dbPath, "fetch", "--force", server.URL)
	if err != nil {
		t.Errorf("Force fetch failed: %v, output: %s", err, output3)
	}

	if count := atomic.LoadInt64(&requestCount); count != 3 {
		t.Errorf("Expected 3 requests after force fetch, got %d", count)
	}
}

func TestIntegrationConcurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Build the feedspool binary
	binaryPath := buildBinary(t)
	defer os.Remove(binaryPath)

	// Create temporary directory
	testDir := t.TempDir()

	dbPath := filepath.Join(testDir, "feeds.db")

	// Create multiple test servers
	servers := make([]*httptest.Server, 3)
	for i := 0; i < 3; i++ {
		feedTitle := "Test Feed " + string(rune('1'+i))
		feedXML := strings.ReplaceAll(integrationTestFeed, "Integration Test Feed", feedTitle)

		servers[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			// Add small delay to test concurrency
			time.Sleep(100 * time.Millisecond)
			w.Header().Set("Content-Type", "application/rss+xml")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(feedXML))
		}))
	}

	// Clean up servers
	defer func() {
		for _, server := range servers {
			server.Close()
		}
	}()

	// Create OPML with multiple feeds
	opmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
    <head>
        <title>Concurrent Test OPML</title>
    </head>
    <body>`

	for i, server := range servers {
		feedTitle := "Test Feed " + string(rune('1'+i))
		opmlContent += `
        <outline text="` + feedTitle + `" type="rss" xmlUrl="` + server.URL + `" />`
	}

	opmlContent += `
    </body>
</opml>`

	opmlPath := filepath.Join(testDir, "concurrent_feeds.opml")
	err := os.WriteFile(opmlPath, []byte(opmlContent), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	// Initialize database
	runCommand(binaryPath, "--database", dbPath, "init")

	// Measure time for concurrent fetch
	start := time.Now()
	output, err := runCommand(binaryPath, "--database", dbPath, "fetch", "--format", "opml", "--filename", opmlPath, "--concurrency", "3")
	duration := time.Since(start)

	if err != nil {
		t.Errorf("Concurrent fetch failed: %v, output: %s", err, output)
	}

	// Should complete in less than 500ms if truly concurrent (3 * 100ms + overhead)
	// If sequential it would take 3 * 100ms + more overhead = ~400ms+
	if duration > 500*time.Millisecond {
		t.Errorf("Concurrent fetch took too long: %v (should be concurrent)", duration)
	}

	// Verify all feeds were processed
	for _, server := range servers {
		showOutput, err := runCommand(binaryPath, "--database", dbPath, "show", server.URL)
		if err != nil {
			t.Errorf("Failed to show feed after concurrent fetch: %v", err)
		}

		if !strings.Contains(showOutput, "Integration Test Item") {
			t.Errorf("Feed should be present after concurrent fetch, got: %s", showOutput)
		}
	}
}

// Helper functions

func buildBinary(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "feedspool")

	cmd := exec.Command("go", "build", "-o", binaryPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to build binary: %v, output: %s", err, output)
	}

	return binaryPath
}

func runCommand(binary string, args ...string) (string, error) {
	cmd := exec.Command(binary, args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

const integrationMaxAgeWindow = "168h"

// integrationSharedItemTitle names the item served by the "shared" feed used in
// TestMultiSiteDirectoryBuild. It is asserted to appear on both sites that
// reference the shared feed, proving the deduped fetch's content actually
// reaches every site that subscribes to it.
const integrationSharedItemTitle = "Integration Item"

// integrationSoloItemTitle names the item served only by the "solo" feed,
// which only one site subscribes to. It must never appear on a site that
// doesn't reference the solo feed, which is what makes the shared-item
// assertion above falsifiable rather than incidentally true.
const integrationSoloItemTitle = "Solo-Only Item"

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

// integrationSoloFeedXMLTemplate is formatted with integrationSoloItemTitle
// rather than embedding the title literally, so the title constant stays the
// single source of truth (and goconst has nothing to flag).
const integrationSoloFeedXMLTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
    <channel>
        <title>Solo Feed</title>
        <description>A feed only one site subscribes to</description>
        <link>https://example.com</link>
        <item>
            <title>%s</title>
            <link>https://example.com/solo-only-item</link>
            <description>An item only the solo feed serves</description>
            <guid>solo-only-item-1</guid>
        </item>
    </channel>
</rss>`

func TestMultiSiteDirectoryBuild(t *testing.T) {
	integrationSoloFeedXML := fmt.Sprintf(integrationSoloFeedXMLTemplate, integrationSoloItemTitle)

	var sharedHits int32

	shared := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&sharedHits, 1)
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(integrationFeedXML))
	}))
	defer shared.Close()

	solo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(integrationSoloFeedXML))
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
		MaxAge:    integrationMaxAgeWindow,
		OutputDir: outDir,
		Database:  dbPath,
		Quiet:     true,
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
	for _, want := range []string{`href="alpha/"`, `href="beta/"`, "Alpha", "Beta", "2 feeds", "1 feed<"} {
		if !strings.Contains(string(indexHTML), want) {
			t.Errorf("index.html does not contain %q", want)
		}
	}
	if strings.Contains(string(indexHTML), "1 feeds") {
		t.Error("index.html incorrectly pluralized a feed count of 1 as \"1 feeds\"")
	}

	// The dedup payoff: one fetch of the shared feed must serve both sites that
	// reference it. Item content is rendered into per-feed fragment pages
	// under <site>/feeds/, not inlined into <site>/index.html (which is a
	// shell that lazy-loads those fragments), so search the whole site
	// output tree rather than just the shell page. The solo item's content
	// is checked as a control: alpha (which subscribes to both feeds) must
	// have it, but beta (which only subscribes to the shared feed) must not,
	// or the shared-item match above would be proving nothing (e.g. if it
	// were satisfied by shared site chrome instead of actual item content).
	if !siteTreeContains(t, outDir, "alpha", integrationSharedItemTitle) {
		t.Errorf("alpha site output does not contain shared item title %q", integrationSharedItemTitle)
	}
	if !siteTreeContains(t, outDir, "beta", integrationSharedItemTitle) {
		t.Errorf("beta site output does not contain shared item title %q; "+
			"the deduped fetch did not reach both sites", integrationSharedItemTitle)
	}
	if !siteTreeContains(t, outDir, "alpha", integrationSoloItemTitle) {
		t.Errorf("alpha site output does not contain solo item title %q", integrationSoloItemTitle)
	}
	if siteTreeContains(t, outDir, "beta", integrationSoloItemTitle) {
		t.Errorf("beta site output unexpectedly contains solo item title %q; "+
			"beta does not subscribe to the solo feed", integrationSoloItemTitle)
	}

	// Removing a list must prune its directory and drop it from the index.
	if err := os.Remove(filepath.Join(listDir, "beta.opml")); err != nil {
		t.Fatal(err)
	}
	if _, err := sitegroup.RenderAll(listDir, &renderer.WorkflowConfig{
		MaxAge:    integrationMaxAgeWindow,
		OutputDir: outDir,
		Database:  dbPath,
		Quiet:     true,
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

// siteTreeContains reports whether any regular file under outDir/slug
// contains want. Item content is rendered into per-feed fragment pages
// (outDir/slug/feeds/*.html) rather than the site's shell index.html, so
// callers checking for rendered item content need to search the whole
// site output tree.
func siteTreeContains(t *testing.T, outDir, slug, want string) bool {
	t.Helper()

	found := false
	root := filepath.Join(outDir, slug)
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(data), want) {
			found = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}

	return found
}
