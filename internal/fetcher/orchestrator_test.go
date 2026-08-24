package fetcher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/database"
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
