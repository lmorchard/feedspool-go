package database

import (
	"database/sql"
	"testing"
	"time"
)

const (
	testAlphaFeedURL   = "https://alpha.example/feed.xml"
	testBetaFeedURL    = "https://beta.example/feed.xml"
	testExampleNetFeed = "https://example.net/rss"
	testAlphaGUID      = "alpha-go"
)

func TestUpsertAndGetFeed(t *testing.T) {
	db := setupTestDB(t)

	feed := &Feed{
		URL:          fixtureFeedURL,
		Title:        fixtureFeedTitle,
		Description:  "Test Description",
		LastUpdated:  time.Now().UTC().Truncate(time.Second),
		ETag:         "test-etag",
		LastModified: fixtureLastModified,
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

func TestSetFeedConfig(t *testing.T) {
	db := setupTestDB(t)
	const (
		feedType  = "scrape"
		selector  = "article.card"
		userAgent = "Scrape Reader/1.0"
	)

	if err := db.SetFeedConfig(fixtureFeedURL, feedType, selector, userAgent); err != nil {
		t.Fatalf("SetFeedConfig() insert error = %v", err)
	}
	feed, err := db.GetFeed(fixtureFeedURL)
	if err != nil {
		t.Fatalf("GetFeed() error = %v", err)
	}
	if feed == nil || feed.Type != feedType || feed.ScrapeSelector != selector || feed.UserAgent != userAgent {
		t.Fatalf("GetFeed() = %#v, want persisted scrape configuration", feed)
	}

	feed.Title = fixtureFeedTitle
	if err := db.UpsertFeed(feed); err != nil {
		t.Fatalf("UpsertFeed() error = %v", err)
	}
	if err := db.SetFeedConfig(fixtureFeedURL, "rss", "", ""); err != nil {
		t.Fatalf("SetFeedConfig() update error = %v", err)
	}
	feed, err = db.GetFeed(fixtureFeedURL)
	if err != nil {
		t.Fatalf("GetFeed() after update error = %v", err)
	}
	if feed.Type != "rss" || feed.ScrapeSelector != "" || feed.UserAgent != "" {
		t.Errorf("updated feed config = %#v, want cleared RSS configuration", feed)
	}
	if feed.Title != fixtureFeedTitle {
		t.Errorf("Title = %q after config update, want preserved %q", feed.Title, fixtureFeedTitle)
	}
}

func TestSetFeedConfigInvalidatesCacheWhenParserChanges(t *testing.T) {
	db := setupTestDB(t)
	recentFetch := time.Now().UTC().Truncate(time.Second)
	feed := &Feed{
		URL:            fixtureFeedURL,
		Type:           FeedTypeScrape,
		ScrapeSelector: ".old",
		ETag:           "old-etag",
		LastModified:   fixtureLastModified,
		LastFetchTime:  recentFetch,
	}
	if err := db.UpsertFeed(feed); err != nil {
		t.Fatal(err)
	}

	if err := db.SetFeedConfig(fixtureFeedURL, FeedTypeScrape, ".old", "Reader/2.0"); err != nil {
		t.Fatal(err)
	}
	unchanged, err := db.GetFeed(fixtureFeedURL)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.ETag != feed.ETag || !unchanged.LastFetchTime.Equal(recentFetch) {
		t.Errorf("unchanged parser config invalidated cache: %#v", unchanged)
	}

	if err := db.SetFeedConfig(fixtureFeedURL, FeedTypeScrape, ".new", "Reader/2.0"); err != nil {
		t.Fatal(err)
	}
	changed, err := db.GetFeed(fixtureFeedURL)
	if err != nil {
		t.Fatal(err)
	}
	if changed.ETag != "" || changed.LastModified != "" || !changed.LastFetchTime.IsZero() {
		t.Errorf("changed parser config retained cache state: %#v", changed)
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

func TestGetFeedSummaries(t *testing.T) {
	const (
		alphaTitle = "Alpha"
		betaTitle  = "Beta"
	)
	db := setupTestDB(t)
	lastFetch := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	feeds := []*Feed{
		{URL: testAlphaFeedURL, Title: alphaTitle, LastFetchTime: lastFetch, ErrorCount: 2},
		{URL: testBetaFeedURL, Title: betaTitle},
	}
	for _, feed := range feeds {
		if err := db.UpsertFeed(feed); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []*Item{
		{FeedURL: feeds[0].URL, GUID: "alpha-1", Title: "One"},
		{FeedURL: feeds[0].URL, GUID: "alpha-2", Title: "Two"},
	} {
		if err := db.UpsertItem(item); err != nil {
			t.Fatal(err)
		}
	}

	summaries, err := db.GetFeedSummaries()
	if err != nil {
		t.Fatalf("GetFeedSummaries() error = %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("GetFeedSummaries() returned %d rows, want 2", len(summaries))
	}
	if summaries[0].URL != feeds[0].URL || summaries[0].ItemCount != 2 ||
		summaries[0].ErrorCount != 2 || !summaries[0].LastFetchTime.Valid ||
		!summaries[0].LastFetchTime.Time.Equal(lastFetch) {
		t.Errorf("first summary = %#v", summaries[0])
	}
	if summaries[1].URL != feeds[1].URL || summaries[1].ItemCount != 0 || summaries[1].LastFetchTime.Valid {
		t.Errorf("second summary = %#v", summaries[1])
	}
}

func TestGetSpoolStatus(t *testing.T) {
	db := setupTestDB(t)
	latestFetch := time.Date(2026, 8, 25, 13, 0, 0, 0, time.UTC)
	feeds := []*Feed{
		{URL: testAlphaFeedURL, LastFetchTime: latestFetch.Add(-time.Hour), ErrorCount: 1},
		{URL: testBetaFeedURL, LastFetchTime: latestFetch, ErrorCount: 2},
	}
	for _, feed := range feeds {
		if err := db.UpsertFeed(feed); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.UpsertItem(&Item{FeedURL: feeds[0].URL, GUID: "one"}); err != nil {
		t.Fatal(err)
	}

	status, err := db.GetSpoolStatus()
	if err != nil {
		t.Fatalf("GetSpoolStatus() error = %v", err)
	}
	if status.FeedCount != 2 || status.ItemCount != 1 || status.FailingFeedCount != 2 ||
		status.ConsecutiveErrorCount != 3 {
		t.Errorf("GetSpoolStatus() = %#v", status)
	}
	if !status.LastFetchTime.Valid || !status.LastFetchTime.Time.Equal(latestFetch) {
		t.Errorf("LastFetchTime = %#v, want %v", status.LastFetchTime, latestFetch)
	}

	empty := setupTestDB(t)
	if err := empty.UpsertFeed(&Feed{URL: "https://never-fetched.example/feed.xml"}); err != nil {
		t.Fatal(err)
	}
	emptyStatus, err := empty.GetSpoolStatus()
	if err != nil {
		t.Fatalf("empty GetSpoolStatus() error = %v", err)
	}
	if emptyStatus.LastFetchTime != (sql.NullTime{}) {
		t.Errorf("empty LastFetchTime = %#v, want invalid", emptyStatus.LastFetchTime)
	}
}

func TestFindFeedsByURLSubstring(t *testing.T) {
	db := setupTestDB(t)
	for _, url := range []string{
		fixtureFeedURL,
		testExampleNetFeed,
		"https://other.test/feed",
	} {
		if err := db.UpsertFeed(&Feed{URL: url}); err != nil {
			t.Fatal(err)
		}
	}

	matches, err := db.FindFeedsByURLSubstring("EXAMPLE")
	if err != nil {
		t.Fatalf("FindFeedsByURLSubstring() error = %v", err)
	}
	if len(matches) != 2 || matches[0].URL != fixtureFeedURL ||
		matches[1].URL != testExampleNetFeed {
		t.Errorf("FindFeedsByURLSubstring() = %#v", matches)
	}

	literal, err := db.FindFeedsByURLSubstring("%.com")
	if err != nil {
		t.Fatalf("literal FindFeedsByURLSubstring() error = %v", err)
	}
	if len(literal) != 0 {
		t.Errorf("literal FindFeedsByURLSubstring() = %#v, want no wildcard matches", literal)
	}
}
