package database

import (
	"testing"
	"time"
)

func TestUpsertAndGetFeed(t *testing.T) {
	db := setupTestDB(t)

	feed := &Feed{
		URL:          fixtureFeedURL,
		Title:        fixtureFeedTitle,
		Description:  "Test Description",
		LastUpdated:  time.Now().UTC().Truncate(time.Second),
		ETag:         "test-etag",
		LastModified: "Mon, 01 Jan 2024 00:00:00 GMT",
		FeedJSON:     JSON(`{"title": "Test Feed"}`),
	}

	// Test Upsert
	err := db.UpsertFeed(feed)
	if err != nil {
		t.Errorf("UpsertFeed() error = %v", err)
	}

	// Test Get
	retrieved, err := db.GetFeed(feed.URL)
	if err != nil {
		t.Errorf("db.GetFeed() error = %v", err)
	}

	if retrieved == nil {
		t.Fatal("db.GetFeed() returned nil")
	}

	if retrieved.URL != feed.URL {
		t.Errorf("Retrieved feed URL = %v, want %v", retrieved.URL, feed.URL)
	}

	if retrieved.Title != feed.Title {
		t.Errorf("Retrieved feed Title = %v, want %v", retrieved.Title, feed.Title)
	}

	if retrieved.ETag != feed.ETag {
		t.Errorf("Retrieved feed ETag = %v, want %v", retrieved.ETag, feed.ETag)
	}
}

func TestGetFeedNotFound(t *testing.T) {
	db := setupTestDB(t)

	feed, err := db.GetFeed("https://nonexistent.com/feed.xml")
	if err != nil {
		t.Errorf("db.GetFeed() error = %v", err)
	}

	if feed != nil {
		t.Errorf("db.GetFeed() should return nil for non-existent feed")
	}
}

func TestSetFeedUserAgent(t *testing.T) {
	db := setupTestDB(t)
	const customUserAgent = "Custom Reader/1.0"

	if err := db.SetFeedUserAgent(fixtureFeedURL, customUserAgent); err != nil {
		t.Fatalf("SetFeedUserAgent() insert error = %v", err)
	}
	feed, err := db.GetFeed(fixtureFeedURL)
	if err != nil {
		t.Fatalf("GetFeed() error = %v", err)
	}
	if feed == nil || feed.UserAgent != customUserAgent {
		t.Fatalf("GetFeed() = %#v, want UserAgent %q", feed, customUserAgent)
	}

	feed.Title = fixtureFeedTitle
	if err := db.UpsertFeed(feed); err != nil {
		t.Fatalf("UpsertFeed() error = %v", err)
	}
	if err := db.SetFeedUserAgent(fixtureFeedURL, ""); err != nil {
		t.Fatalf("SetFeedUserAgent() clear error = %v", err)
	}
	feed, err = db.GetFeed(fixtureFeedURL)
	if err != nil {
		t.Fatalf("GetFeed() after clear error = %v", err)
	}
	if feed.UserAgent != "" {
		t.Errorf("cleared UserAgent = %q, want empty string", feed.UserAgent)
	}
	if feed.Title != fixtureFeedTitle {
		t.Errorf("Title = %q after User-Agent update, want preserved %q", feed.Title, fixtureFeedTitle)
	}
}

func TestGetAllFeeds(t *testing.T) {
	db := setupTestDB(t)

	feeds := []*Feed{
		{
			URL:      fixtureFeedURL1,
			Title:    "Feed 1",
			FeedJSON: JSON(`{"title": "Feed 1"}`),
		},
		{
			URL:      "https://example2.com/feed.xml",
			Title:    "Feed 2",
			FeedJSON: JSON(`{"title": "Feed 2"}`),
		},
	}

	// Insert feeds
	for _, feed := range feeds {
		err := db.UpsertFeed(feed)
		if err != nil {
			t.Errorf("UpsertFeed() error = %v", err)
		}
	}

	// Get all feeds
	retrieved, err := db.GetAllFeeds()
	if err != nil {
		t.Errorf("db.GetAllFeeds() error = %v", err)
	}

	if len(retrieved) != 2 {
		t.Errorf("db.GetAllFeeds() returned %d feeds, want 2", len(retrieved))
	}

	// Check ordering (should be by URL)
	if retrieved[0].URL != fixtureFeedURL1 {
		t.Errorf("First feed URL = %v, want %v", retrieved[0].URL, fixtureFeedURL1)
	}
}
