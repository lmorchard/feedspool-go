package database

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTestDB(t *testing.T) (db *DB, tempDir string) {
	t.Helper()

	// Create temporary database file
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "feedspool_test.db")

	// Initialize database
	db, err := New(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := db.InitSchema(); err != nil {
		t.Fatal(err)
	}

	// Cleanup function
	t.Cleanup(func() {
		db.Close()
		os.Remove(dbPath)
	})

	return db, dbPath
}

func TestUpsertAndGetFeed(t *testing.T) {
	db, _ := setupTestDB(t)

	feed := &Feed{
		URL:          "https://example.com/feed.xml",
		Title:        "Test Feed",
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
	db, _ := setupTestDB(t)

	feed, err := db.GetFeed("https://nonexistent.com/feed.xml")
	if err != nil {
		t.Errorf("db.GetFeed() error = %v", err)
	}

	if feed != nil {
		t.Errorf("db.GetFeed() should return nil for non-existent feed")
	}
}

func TestGetAllFeeds(t *testing.T) {
	db, _ := setupTestDB(t)

	feeds := []*Feed{
		{
			URL:      "https://example1.com/feed.xml",
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
	if retrieved[0].URL != "https://example1.com/feed.xml" {
		t.Errorf("First feed URL = %v, want %v", retrieved[0].URL, "https://example1.com/feed.xml")
	}
}

func TestUpsertAndGetItem(t *testing.T) {
	db, _ := setupTestDB(t)

	// First insert a feed
	feed := &Feed{
		URL:      "https://example.com/feed.xml",
		Title:    "Test Feed",
		FeedJSON: JSON(`{"title": "Test Feed"}`),
	}
	err := db.UpsertFeed(feed)
	if err != nil {
		t.Fatal(err)
	}

	item := &Item{
		FeedURL:       "https://example.com/feed.xml",
		GUID:          "test-guid",
		Title:         "Test Item",
		Link:          "https://example.com/item",
		PublishedDate: time.Now().UTC().Truncate(time.Second),
		Content:       "Test content",
		Summary:       "Test summary",
		ItemJSON:      JSON(`{"title": "Test Item"}`),
	}

	// Test Upsert
	err = db.UpsertItem(item)
	if err != nil {
		t.Errorf("db.UpsertItem() error = %v", err)
	}

	// Test Get
	items, err := db.GetItemsForFeed(item.FeedURL, 0, time.Time{}, time.Time{})
	if err != nil {
		t.Errorf("db.GetItemsForFeed() error = %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("db.GetItemsForFeed() returned %d items, want 1", len(items))
	}

	retrieved := items[0]
	if retrieved.GUID != item.GUID {
		t.Errorf("Retrieved item GUID = %v, want %v", retrieved.GUID, item.GUID)
	}

	if retrieved.Title != item.Title {
		t.Errorf("Retrieved item Title = %v, want %v", retrieved.Title, item.Title)
	}
}

func TestGetItemsForFeedWithFilters(t *testing.T) {
	const testItem3GUID = "item3"

	db, _ := setupTestDB(t)

	// Insert feed
	feed := &Feed{
		URL:      "https://example.com/feed.xml",
		Title:    "Test Feed",
		FeedJSON: JSON(`{"title": "Test Feed"}`),
	}
	err := db.UpsertFeed(feed)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Second)

	items := []*Item{
		{
			FeedURL:       feed.URL,
			GUID:          "item1",
			Title:         "Item 1",
			PublishedDate: now.Add(-2 * time.Hour),
			ItemJSON:      JSON(`{"title": "Item 1"}`),
		},
		{
			FeedURL:       feed.URL,
			GUID:          "item2",
			Title:         "Item 2",
			PublishedDate: now.Add(-1 * time.Hour),
			ItemJSON:      JSON(`{"title": "Item 2"}`),
		},
		{
			FeedURL:       feed.URL,
			GUID:          testItem3GUID,
			Title:         "Item 3",
			PublishedDate: now,
			ItemJSON:      JSON(`{"title": "Item 3"}`),
		},
	}

	// Insert items
	for _, item := range items {
		err := db.UpsertItem(item)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Test limit
	retrieved, err := db.GetItemsForFeed(feed.URL, 2, time.Time{}, time.Time{})
	if err != nil {
		t.Errorf("db.GetItemsForFeed() error = %v", err)
	}

	if len(retrieved) != 2 {
		t.Errorf("db.GetItemsForFeed() with limit=2 returned %d items, want 2", len(retrieved))
	}

	// Should be ordered by newest first
	if retrieved[0].GUID != testItem3GUID {
		t.Errorf("First item GUID = %v, want %s", retrieved[0].GUID, testItem3GUID)
	}

	// Test since filter
	since := now.Add(-30 * time.Minute)
	retrieved, err = db.GetItemsForFeed(feed.URL, 0, since, time.Time{})
	if err != nil {
		t.Errorf("db.GetItemsForFeed() error = %v", err)
	}

	if len(retrieved) != 1 {
		t.Errorf("db.GetItemsForFeed() with since filter returned %d items, want 1", len(retrieved))
	}

	if retrieved[0].GUID != testItem3GUID {
		t.Errorf("Filtered item GUID = %v, want %s", retrieved[0].GUID, testItem3GUID)
	}
}

func TestMarkItemsArchived(t *testing.T) {
	db, _ := setupTestDB(t)

	// Insert feed
	feed := &Feed{
		URL:      "https://example.com/feed.xml",
		Title:    "Test Feed",
		FeedJSON: JSON(`{"title": "Test Feed"}`),
	}
	err := db.UpsertFeed(feed)
	if err != nil {
		t.Fatal(err)
	}

	// Insert items
	items := []string{"item1", "item2", "item3"}
	for _, guid := range items {
		item := &Item{
			FeedURL:       feed.URL,
			GUID:          guid,
			Title:         guid,
			PublishedDate: time.Now(),
			ItemJSON:      JSON(`{"title": "` + guid + `"}`),
		}
		err := db.UpsertItem(item)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Mark item2 and item3 as not archived (active), item1 should be archived
	activeGUIDs := []string{"item2", "item3"}
	err = db.MarkItemsArchived(feed.URL, activeGUIDs)
	if err != nil {
		t.Errorf("db.MarkItemsArchived() error = %v", err)
	}

	// Get all items (including archived)
	conn := db.GetConnection()
	rows, err := conn.Query("SELECT guid, archived FROM items WHERE feed_url = ? ORDER BY guid", feed.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	results := make(map[string]bool)
	for rows.Next() {
		var guid string
		var archived bool
		err := rows.Scan(&guid, &archived)
		if err != nil {
			t.Fatal(err)
		}
		results[guid] = archived
	}

	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	if !results["item1"] {
		t.Errorf("item1 should be archived")
	}

	if results["item2"] {
		t.Errorf("item2 should not be archived")
	}

	if results["item3"] {
		t.Errorf("item3 should not be archived")
	}
}

func TestDeleteArchivedItems(t *testing.T) {
	db, _ := setupTestDB(t)

	// Insert feed
	feed := &Feed{
		URL:      "https://example.com/feed.xml",
		Title:    "Test Feed",
		FeedJSON: JSON(`{"title": "Test Feed"}`),
	}
	err := db.UpsertFeed(feed)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Second)

	// Insert items - one old archived, one recent archived, one not archived
	items := []*Item{
		{
			FeedURL:       feed.URL,
			GUID:          "old-archived",
			Title:         "Old Archived",
			PublishedDate: now.Add(-2 * time.Hour),
			Archived:      true,
			ItemJSON:      JSON(`{"title": "Old Archived"}`),
		},
		{
			FeedURL:       feed.URL,
			GUID:          "recent-archived",
			Title:         "Recent Archived",
			PublishedDate: now.Add(-30 * time.Minute),
			Archived:      true,
			ItemJSON:      JSON(`{"title": "Recent Archived"}`),
		},
		{
			FeedURL:       feed.URL,
			GUID:          "not-archived",
			Title:         "Not Archived",
			PublishedDate: now.Add(-2 * time.Hour),
			Archived:      false,
			ItemJSON:      JSON(`{"title": "Not Archived"}`),
		},
	}

	for _, item := range items {
		err := db.UpsertItem(item)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Delete archived items older than 1 hour
	cutoff := now.Add(-1 * time.Hour)
	deleted, err := db.DeleteArchivedItems(cutoff)
	if err != nil {
		t.Errorf("db.DeleteArchivedItems() error = %v", err)
	}

	if deleted != 1 {
		t.Errorf("db.DeleteArchivedItems() deleted %d items, want 1", deleted)
	}

	// Check remaining items
	allItems, err := db.GetItemsForFeed(feed.URL, 0, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	// Should have 1 non-archived item (archived items are excluded by GetItemsForFeed)
	if len(allItems) != 1 {
		t.Errorf("Found %d non-archived items, want 1", len(allItems))
	}

	// Check total count in database
	conn := db.GetConnection()
	var totalCount int
	err = conn.QueryRow("SELECT COUNT(*) FROM items WHERE feed_url = ?", feed.URL).Scan(&totalCount)
	if err != nil {
		t.Fatal(err)
	}

	if totalCount != 2 { // not-archived + recent-archived
		t.Errorf("Total items in DB = %d, want 2", totalCount)
	}
}
