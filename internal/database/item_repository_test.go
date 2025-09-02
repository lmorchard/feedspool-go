package database

import (
	"testing"
	"time"
)

func TestUpsertAndGetItem(t *testing.T) {
	db := setupTestDB(t)

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

func TestUpsertItemDateStability(t *testing.T) {
	const updatedTitle = "Updated Title"

	db := setupTestDB(t)

	// Insert feed first
	feed := &Feed{
		URL:      "https://example.com/feed.xml",
		Title:    "Test Feed",
		FeedJSON: JSON(`{"title": "Test Feed"}`),
	}
	err := db.UpsertFeed(feed)
	if err != nil {
		t.Fatal(err)
	}

	// Create an item with a specific published date
	originalTime := time.Date(2023, 1, 15, 12, 0, 0, 0, time.UTC)
	item := &Item{
		FeedURL:       feed.URL,
		GUID:          "test-item-1",
		Title:         "Test Item",
		Link:          "https://example.com/item1",
		PublishedDate: originalTime,
		Content:       "Test content",
		ItemJSON:      JSON(`{"title": "Test Item"}`),
	}

	// First upsert (insert)
	err = db.UpsertItem(item)
	if err != nil {
		t.Errorf("First UpsertItem() error = %v", err)
	}

	// Get the item to verify the date
	items, err := db.GetItemsForFeed(feed.URL, 0, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatal("Expected 1 item")
	}

	firstInsertDate := items[0].PublishedDate

	// Wait a moment, then update the item with a new date (simulating a feed without proper dates)
	time.Sleep(10 * time.Millisecond)
	newTime := time.Now() // This should NOT overwrite the original date
	item.PublishedDate = newTime
	item.Title = updatedTitle // Update other fields

	// Second upsert (update)
	err = db.UpsertItem(item)
	if err != nil {
		t.Errorf("Second UpsertItem() error = %v", err)
	}

	// Get the item again and verify the date is stable
	items, err = db.GetItemsForFeed(feed.URL, 0, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatal("Expected 1 item")
	}

	secondFetchDate := items[0].PublishedDate

	// The published date should NOT have changed
	if !firstInsertDate.Equal(secondFetchDate) {
		t.Errorf("Published date should be stable across updates. First: %v, Second: %v",
			firstInsertDate, secondFetchDate)
	}

	// But other fields should be updated
	if items[0].Title != updatedTitle {
		t.Errorf("Title should be updated: got %s, want %s", items[0].Title, updatedTitle)
	}
}

func TestGetItemsForFeedWithFilters(t *testing.T) {
	const testItem3GUID = "item3"

	db := setupTestDB(t)

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
	db := setupTestDB(t)

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
	db := setupTestDB(t)

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
