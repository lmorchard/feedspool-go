package fetcher

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
)

const testFeedXML = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
    <channel>
        <title>Test Feed</title>
        <description>A test RSS feed</description>
        <link>https://example.com</link>
        <item>
            <title>Test Item 1</title>
            <link>https://example.com/item1</link>
            <description>First test item</description>
            <pubDate>Mon, 01 Jan 2024 12:00:00 GMT</pubDate>
            <guid>item-1</guid>
        </item>
        <item>
            <title>Test Item 2</title>
            <link>https://example.com/item2</link>
            <description>Second test item</description>
            <pubDate>Mon, 01 Jan 2024 13:00:00 GMT</pubDate>
            <guid>item-2</guid>
        </item>
    </channel>
</rss>`

func setupTestDatabase(t *testing.T) {
	t.Helper()

	// Create temporary database file
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "fetcher_test.db")

	// Initialize database
	if err := database.Connect(dbPath); err != nil {
		t.Fatal(err)
	}

	if err := database.InitSchema(); err != nil {
		t.Fatal(err)
	}

	// Cleanup function
	t.Cleanup(func() {
		database.Close()
		os.Remove(dbPath)
	})
}

func TestNewFetcher(t *testing.T) {
	timeout := 30 * time.Second
	maxItems := 50
	force := true

	fetcher := NewFetcher(timeout, maxItems, force)

	if fetcher.timeout != timeout {
		t.Errorf("NewFetcher() timeout = %v, want %v", fetcher.timeout, timeout)
	}

	if fetcher.maxItems != maxItems {
		t.Errorf("NewFetcher() maxItems = %v, want %v", fetcher.maxItems, maxItems)
	}

	if fetcher.forceFlag != force {
		t.Errorf("NewFetcher() forceFlag = %v, want %v", fetcher.forceFlag, force)
	}

	if fetcher.client.Timeout != timeout {
		t.Errorf("NewFetcher() client.Timeout = %v, want %v", fetcher.client.Timeout, timeout)
	}
}

func TestFetchFeedSuccess(t *testing.T) {
	const testETag = "test-etag"

	setupTestDatabase(t)

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Header().Set("ETag", testETag)
		w.Header().Set("Last-Modified", "Mon, 01 Jan 2024 12:00:00 GMT")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(testFeedXML))
	}))
	defer server.Close()

	fetcher := NewFetcher(30*time.Second, 100, false)
	result := fetcher.FetchFeed(server.URL)

	if result.Error != nil {
		t.Errorf("FetchFeed() error = %v", result.Error)
	}

	if result.URL != server.URL {
		t.Errorf("FetchFeed() URL = %v, want %v", result.URL, server.URL)
	}

	if result.ItemCount != 2 {
		t.Errorf("FetchFeed() ItemCount = %v, want 2", result.ItemCount)
	}

	if result.Cached {
		t.Errorf("FetchFeed() should not be cached on first fetch")
	}

	if result.Feed == nil {
		t.Fatal("FetchFeed() Feed should not be nil")
	}

	if result.Feed.Title != "Test Feed" {
		t.Errorf("FetchFeed() Feed.Title = %v, want 'Test Feed'", result.Feed.Title)
	}

	if result.Feed.ETag != testETag {
		t.Errorf("FetchFeed() Feed.ETag = %v, want %q", result.Feed.ETag, testETag)
	}
}

func TestFetchFeedNotModified(t *testing.T) {
	const testETag = "test-etag"

	setupTestDatabase(t)

	// Create test server that returns 304 for conditional requests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == testETag {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		w.Header().Set("Content-Type", "application/rss+xml")
		w.Header().Set("ETag", testETag)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(testFeedXML))
	}))
	defer server.Close()

	// First, insert a feed with ETag
	existingFeed := &database.Feed{
		URL:           server.URL,
		Title:         "Existing Feed",
		ETag:          testETag,
		LastFetchTime: time.Now(),
		FeedJSON:      database.JSON(`{"title": "Existing Feed"}`),
	}
	err := database.UpsertFeed(existingFeed)
	if err != nil {
		t.Fatal(err)
	}

	fetcher := NewFetcher(30*time.Second, 100, false)
	result := fetcher.FetchFeed(server.URL)

	if result.Error != nil {
		t.Errorf("FetchFeed() error = %v", result.Error)
	}

	if !result.Cached {
		t.Errorf("FetchFeed() should be cached when 304 returned")
	}

	if result.ItemCount != 0 {
		t.Errorf("FetchFeed() ItemCount = %v, want 0 for cached response", result.ItemCount)
	}
}

func TestFetchFeedHTTPError(t *testing.T) {
	setupTestDatabase(t)

	// Create test server that returns 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	fetcher := NewFetcher(30*time.Second, 100, false)
	result := fetcher.FetchFeed(server.URL)

	if result.Error == nil {
		t.Errorf("FetchFeed() should return error for 404 response")
	}

	if !strings.Contains(result.Error.Error(), "HTTP 404") {
		t.Errorf("FetchFeed() error should mention HTTP 404, got: %v", result.Error)
	}
}

func TestFetchFeedInvalidXML(t *testing.T) {
	setupTestDatabase(t)

	// Create test server that returns invalid XML
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("invalid xml content"))
	}))
	defer server.Close()

	fetcher := NewFetcher(30*time.Second, 100, false)
	result := fetcher.FetchFeed(server.URL)

	if result.Error == nil {
		t.Errorf("FetchFeed() should return error for invalid XML")
	}

	if !strings.Contains(result.Error.Error(), "failed to parse") {
		t.Errorf("FetchFeed() error should mention parsing failure, got: %v", result.Error)
	}
}

func TestFetchFeedMaxItems(t *testing.T) {
	setupTestDatabase(t)

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(testFeedXML))
	}))
	defer server.Close()

	// Limit to 1 item
	fetcher := NewFetcher(30*time.Second, 1, false)
	result := fetcher.FetchFeed(server.URL)

	if result.Error != nil {
		t.Errorf("FetchFeed() error = %v", result.Error)
	}

	if result.ItemCount != 1 {
		t.Errorf("FetchFeed() ItemCount = %v, want 1 (limited by maxItems)", result.ItemCount)
	}
}

func TestFetchFeedForce(t *testing.T) {
	setupTestDatabase(t)

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		// Check that conditional headers are NOT sent when force=true
		if r.Header.Get("If-None-Match") != "" || r.Header.Get("If-Modified-Since") != "" {
			t.Errorf("Conditional headers should not be sent when force=true")
		}

		w.Header().Set("Content-Type", "application/rss+xml")
		w.Header().Set("ETag", "test-etag")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(testFeedXML))
	}))
	defer server.Close()

	// Insert existing feed with ETag
	existingFeed := &database.Feed{
		URL:           server.URL,
		Title:         "Existing Feed",
		ETag:          "test-etag",
		LastFetchTime: time.Now(),
		FeedJSON:      database.JSON(`{"title": "Existing Feed"}`),
	}
	err := database.UpsertFeed(existingFeed)
	if err != nil {
		t.Fatal(err)
	}

	// Fetch with force=true
	fetcher := NewFetcher(30*time.Second, 100, true)
	result := fetcher.FetchFeed(server.URL)

	if result.Error != nil {
		t.Errorf("FetchFeed() error = %v", result.Error)
	}

	if result.Cached {
		t.Errorf("FetchFeed() should not be cached when force=true")
	}

	if requestCount != 1 {
		t.Errorf("Expected 1 request to server, got %d", requestCount)
	}
}

func TestFetchConcurrent(t *testing.T) {
	setupTestDatabase(t)

	// Create test servers
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(strings.ReplaceAll(testFeedXML, "Test Feed", "Feed 1")))
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(strings.ReplaceAll(testFeedXML, "Test Feed", "Feed 2")))
	}))
	defer server2.Close()

	urls := []string{server1.URL, server2.URL}
	results := FetchConcurrent(urls, 2, 30*time.Second, 100, 0, false)

	if len(results) != 2 {
		t.Errorf("FetchConcurrent() returned %d results, want 2", len(results))
	}

	successCount := 0
	for _, result := range results {
		if result.Error == nil {
			successCount++
		}
	}

	if successCount != 2 {
		t.Errorf("FetchConcurrent() had %d successes, want 2", successCount)
	}

	// Check that both feeds were processed
	foundFeed1 := false
	foundFeed2 := false

	for _, result := range results {
		if result.Error == nil && result.Feed != nil {
			if result.Feed.Title == "Feed 1" {
				foundFeed1 = true
			}
			if result.Feed.Title == "Feed 2" {
				foundFeed2 = true
			}
		}
	}

	if !foundFeed1 {
		t.Errorf("FetchConcurrent() did not process Feed 1")
	}

	if !foundFeed2 {
		t.Errorf("FetchConcurrent() did not process Feed 2")
	}
}

func TestFetchConcurrentWithMaxAge(t *testing.T) {
	setupTestDatabase(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(testFeedXML))
	}))
	defer server.Close()

	// Insert recently fetched feed
	recentFeed := &database.Feed{
		URL:           server.URL,
		Title:         "Recent Feed",
		LastFetchTime: time.Now(), // Just fetched
		FeedJSON:      database.JSON(`{"title": "Recent Feed"}`),
	}
	err := database.UpsertFeed(recentFeed)
	if err != nil {
		t.Fatal(err)
	}

	urls := []string{server.URL}
	maxAge := 1 * time.Hour // Skip feeds fetched within last hour

	results := FetchConcurrent(urls, 1, 30*time.Second, 100, maxAge, false)

	if len(results) != 1 {
		t.Errorf("FetchConcurrent() returned %d results, want 1", len(results))
	}

	result := results[0]
	if result.Error != nil {
		t.Errorf("FetchConcurrent() error = %v", result.Error)
	}

	if !result.Cached {
		t.Errorf("FetchConcurrent() should skip recently fetched feed (cached=true)")
	}
}
