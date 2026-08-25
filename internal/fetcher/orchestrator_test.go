package fetcher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/feedlist"
	"github.com/lmorchard/feedspool-go/internal/httpclient"
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

// TestFetchFromURLsEmptyWithRemoveMissingKeepsExistingFeeds guards a
// data-loss bug: FetchFromURLs's early return for an empty/nil URL list is
// the only thing preventing removeMissingFeeds from treating every feed
// already in the database as missing and deleting it. If that early return
// were ever removed or bypassed, RemoveMissing: true with an empty list
// would wipe the whole feeds table.
func TestFetchFromURLsEmptyWithRemoveMissingKeepsExistingFeeds(t *testing.T) {
	db := setupTestDatabase(t)

	const existingFeedURL = "https://example.com/existing-feed.xml"
	if err := db.UpsertFeed(&database.Feed{
		URL:           existingFeedURL,
		Title:         "Existing Feed",
		LastFetchTime: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	o := NewOrchestrator(db, config.GetDefault())
	results := o.FetchFromURLs(context.Background(), nil, FetchOptions{
		Concurrency:   1,
		RemoveMissing: true,
	})

	if len(results) != 0 {
		t.Errorf("len(results) = %d, want 0", len(results))
	}

	urls, err := db.GetFeedURLs()
	if err != nil {
		t.Fatalf("GetFeedURLs() error = %v", err)
	}
	if len(urls) != 1 || urls[0] != existingFeedURL {
		t.Errorf("GetFeedURLs() = %v, want [%s] (empty URL list + RemoveMissing must not delete existing feeds)",
			urls, existingFeedURL)
	}
}

func TestFetchFromFileSynchronizesUserAgent(t *testing.T) {
	db := setupTestDatabase(t)
	userAgents := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgents <- r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(testFeedXML))
	}))
	defer server.Close()

	filename := filepath.Join(t.TempDir(), "feeds.opml")
	writeList := func(userAgentAttribute string) {
		t.Helper()
		content := `<opml version="2.0"><body><outline type="rss" xmlUrl="` +
			server.URL + `"` + userAgentAttribute + ` /></body></opml>`
		if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	o := NewOrchestrator(db, config.GetDefault())
	opts := FetchOptions{
		Timeout:     config.DefaultTimeout,
		MaxItems:    config.DefaultMaxItems,
		Concurrency: 1,
		Force:       true,
	}

	const customUserAgent = "Protected Reader/1.0"
	writeList(` userAgent="` + customUserAgent + `"`)
	results, err := o.FetchFromFile(context.Background(), feedlist.FormatOPML, filename, opts)
	if err != nil {
		t.Fatalf("FetchFromFile() error = %v", err)
	}
	if len(results) != 1 || results[0].Error != nil {
		t.Fatalf("FetchFromFile() results = %#v, want one successful result", results)
	}
	if got := <-userAgents; got != customUserAgent {
		t.Errorf("first request User-Agent = %q, want %q", got, customUserAgent)
	}

	writeList("")
	results, err = o.FetchFromFile(context.Background(), feedlist.FormatOPML, filename, opts)
	if err != nil {
		t.Fatalf("FetchFromFile() after clear error = %v", err)
	}
	if len(results) != 1 || results[0].Error != nil {
		t.Fatalf("FetchFromFile() after clear results = %#v, want one successful result", results)
	}
	if got := <-userAgents; got != httpclient.DefaultUserAgent {
		t.Errorf("request User-Agent after clear = %q, want default %q", got, httpclient.DefaultUserAgent)
	}

	feed, err := db.GetFeed(server.URL)
	if err != nil {
		t.Fatalf("GetFeed() error = %v", err)
	}
	if feed.UserAgent != "" {
		t.Errorf("database UserAgent after clear = %q, want empty string", feed.UserAgent)
	}
}

func TestFetchFromFileRejectsConflictingUserAgents(t *testing.T) {
	db := setupTestDatabase(t)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(testFeedXML))
	}))
	defer server.Close()

	filename := filepath.Join(t.TempDir(), "feeds.opml")
	content := `<opml version="2.0"><body>` +
		`<outline xmlUrl="` + server.URL + `" userAgent="First Reader/1.0" />` +
		`<outline xmlUrl="` + server.URL + `" userAgent="Second Reader/2.0" />` +
		`</body></opml>`
	if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	o := NewOrchestrator(db, config.GetDefault())
	_, err := o.FetchFromFile(context.Background(), feedlist.FormatOPML, filename, FetchOptions{
		Timeout:     config.DefaultTimeout,
		MaxItems:    config.DefaultMaxItems,
		Concurrency: 1,
	})
	if err == nil {
		t.Fatal("FetchFromFile() error = nil, want conflicting User-Agent error")
	}
	if hits.Load() != 0 {
		t.Errorf("server received %d requests, want 0 after configuration conflict", hits.Load())
	}
}
