package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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

	t.Run("update_from_opml", func(t *testing.T) {
		output, err := runCommand(binaryPath, "--database", dbPath, "update", opmlPath)
		if err != nil {
			t.Errorf("Update failed: %v, output: %s", err, output)
		}

		if !strings.Contains(output, "Found") && !strings.Contains(output, "Update Summary") {
			t.Errorf("Update output should show results, got: %s", output)
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

	// Measure time for concurrent update
	start := time.Now()
	output, err := runCommand(binaryPath, "--database", dbPath, "update", "--concurrency", "3", opmlPath)
	duration := time.Since(start)

	if err != nil {
		t.Errorf("Concurrent update failed: %v, output: %s", err, output)
	}

	// Should complete in less than 500ms if truly concurrent (3 * 100ms + overhead)
	// If sequential it would take 3 * 100ms + more overhead = ~400ms+
	if duration > 500*time.Millisecond {
		t.Errorf("Concurrent update took too long: %v (should be concurrent)", duration)
	}

	// Verify all feeds were processed
	for _, server := range servers {
		showOutput, err := runCommand(binaryPath, "--database", dbPath, "show", server.URL)
		if err != nil {
			t.Errorf("Failed to show feed after concurrent update: %v", err)
		}

		if !strings.Contains(showOutput, "Integration Test Item") {
			t.Errorf("Feed should be present after concurrent update, got: %s", showOutput)
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
