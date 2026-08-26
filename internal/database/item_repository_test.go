package database

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/lmorchard/feedspool-go/internal/itemtext"
)

func TestUpsertAndGetItem(t *testing.T) {
	db := setupTestDB(t)

	// First insert a feed
	feed := &Feed{
		URL:      fixtureFeedURL,
		Title:    fixtureFeedTitle,
		FeedJSON: JSON(`{"title": "Test Feed"}`),
	}
	err := db.UpsertFeed(feed)
	if err != nil {
		t.Fatal(err)
	}

	item := &Item{
		FeedURL:       fixtureFeedURL,
		GUID:          fixtureGUID,
		Title:         testItemTitle,
		Link:          fixtureItemLink,
		PublishedDate: time.Now().UTC().Truncate(time.Second),
		Content:       fixtureItemContent,
		Summary:       fixtureItemSummary,
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

func TestGetItemsByLinkAndIdentity(t *testing.T) {
	const (
		alphaFeedURL = "https://alpha.example/feed"
		alphaGUID    = "alpha-guid"
		alphaTitle   = "Alpha"
		betaFeedURL  = "https://beta.example/feed"
		betaGUID     = "beta-guid"
		betaTitle    = "Beta"
		sharedLink   = "https://example.com/shared"
	)
	db := setupTestDB(t)
	for _, feedURL := range []string{alphaFeedURL, betaFeedURL} {
		if err := db.UpsertFeed(&Feed{URL: feedURL, Title: feedURL}); err != nil {
			t.Fatal(err)
		}
	}

	firstSeen := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	items := []*Item{
		{
			FeedURL:   betaFeedURL,
			GUID:      betaGUID,
			Title:     betaTitle,
			Link:      sharedLink,
			FirstSeen: sql.NullTime{Time: firstSeen, Valid: true},
		},
		{
			FeedURL:       alphaFeedURL,
			GUID:          alphaGUID,
			Title:         alphaTitle,
			Link:          sharedLink,
			PublishedDate: firstSeen.Add(-time.Hour),
		},
	}
	for _, item := range items {
		if err := db.UpsertItem(item); err != nil {
			t.Fatal(err)
		}
	}

	matches, err := db.GetItemsByLink(sharedLink)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("GetItemsByLink() returned %d items, want 2", len(matches))
	}
	if matches[0].GUID != alphaGUID || matches[1].GUID != betaGUID {
		t.Fatalf("GetItemsByLink() order = [%s, %s], want [alpha-guid, beta-guid]",
			matches[0].GUID, matches[1].GUID)
	}
	if !matches[1].PublishedDate.IsZero() || !matches[1].EffectiveDate().Equal(firstSeen) {
		t.Errorf("scraped item dates = published %v, effective %v", matches[1].PublishedDate, matches[1].EffectiveDate())
	}

	item, err := db.GetItem(betaFeedURL, betaGUID)
	if err != nil {
		t.Fatal(err)
	}
	if item == nil || item.Title != betaTitle {
		t.Fatalf("GetItem() = %#v, want beta item", item)
	}
	missing, err := db.GetItem(betaFeedURL, "missing")
	if err != nil {
		t.Fatal(err)
	}
	if missing != nil {
		t.Fatalf("GetItem() missing = %#v, want nil", missing)
	}
}

func TestUpsertItemDateStability(t *testing.T) {
	const updatedTitle = "Updated Title"

	db := setupTestDB(t)

	// Insert feed first
	feed := &Feed{
		URL:      fixtureFeedURL,
		Title:    fixtureFeedTitle,
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
		Title:         testItemTitle,
		Link:          "https://example.com/item1",
		PublishedDate: originalTime,
		Content:       fixtureItemContent,
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
	const testItem3GUID = fixtureGUID3

	db := setupTestDB(t)

	// Insert feed
	feed := &Feed{
		URL:      fixtureFeedURL,
		Title:    fixtureFeedTitle,
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
			GUID:          fixtureGUID1,
			Title:         "Item 1",
			PublishedDate: now.Add(-2 * time.Hour),
			ItemJSON:      JSON(`{"title": "Item 1"}`),
		},
		{
			FeedURL:       feed.URL,
			GUID:          fixtureGUID2,
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

func TestGetItemsForFeedUsesUTCFirstSeenFallback(t *testing.T) {
	db := setupTestDB(t)
	if err := db.UpsertFeed(&Feed{URL: fixtureFeedURL, Title: fixtureFeedTitle}); err != nil {
		t.Fatal(err)
	}
	firstSeen := time.Date(2026, time.August, 25, 20, 0, 0, 0, time.UTC)
	if err := db.UpsertItem(&Item{
		FeedURL:   fixtureFeedURL,
		GUID:      fixtureGUID,
		Title:     testItemTitle,
		Link:      fixtureItemLink,
		FirstSeen: sql.NullTime{Time: firstSeen, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}

	localZone := time.FixedZone("PDT", -7*60*60)
	since := time.Date(2026, time.August, 25, 12, 30, 0, 0, localZone)
	until := time.Date(2026, time.August, 25, 13, 30, 0, 0, localZone)
	items, err := db.GetItemsForFeed(
		fixtureFeedURL,
		0,
		since,
		until,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("len(GetItemsForFeed()) = %d, want 1 across timezone offsets", len(items))
	}
	if got := items[0].EffectiveDate(); got.Location() != time.UTC || got.Hour() != 20 {
		t.Errorf("EffectiveDate() = %v, want 20:00 UTC", got)
	}

	filtered, err := db.GetItems(&ItemFilter{Since: since, Until: until})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 {
		t.Fatalf("len(GetItems()) = %d, want 1 across timezone offsets", len(filtered))
	}
}

func TestEffectiveDateQueriesHandleLegacyOffsetsConservatively(t *testing.T) {
	const afterCutoffGUID = "after-cutoff"
	db := setupTestDB(t)
	if err := db.UpsertFeed(&Feed{URL: fixtureFeedURL, Title: fixtureFeedTitle}); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		guid          string
		publishedDate string
	}{
		{guid: "before-cutoff", publishedDate: "2026-08-25T12:00:00-07:00"},
		{guid: afterCutoffGUID, publishedDate: "2026-08-25T13:00:00-07:00"},
		{guid: "unparseable", publishedDate: "not-a-date"},
	} {
		_, err := db.conn.Exec(`
			INSERT INTO items (feed_url, guid, published_date, archived)
			VALUES (?, ?, ?, 1)
		`, fixtureFeedURL, item.guid, item.publishedDate)
		if err != nil {
			t.Fatal(err)
		}
	}

	cutoff := time.Date(2026, 8, 25, 19, 45, 0, 0, time.UTC)
	items, err := db.GetItemsForFeed(fixtureFeedURL, 0, cutoff, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].GUID != afterCutoffGUID {
		t.Fatalf("GetItemsForFeed() = %#v, want only after-cutoff; unparseable dates cannot be placed in a window", items)
	}

	deleted, err := db.DeleteArchivedItems(cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("DeleteArchivedItems() deleted %d items, want only before-cutoff", deleted)
	}
	var remaining int
	if err := db.conn.QueryRow(`
		SELECT COUNT(*) FROM items WHERE guid IN ('after-cutoff', 'unparseable')
	`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 2 {
		t.Fatalf("remaining protected items = %d, want 2", remaining)
	}
}

func TestMarkItemsArchived(t *testing.T) {
	db := setupTestDB(t)

	// Insert feed
	feed := &Feed{
		URL:      fixtureFeedURL,
		Title:    fixtureFeedTitle,
		FeedJSON: JSON(`{"title": "Test Feed"}`),
	}
	err := db.UpsertFeed(feed)
	if err != nil {
		t.Fatal(err)
	}

	// Insert items
	items := []string{fixtureGUID1, fixtureGUID2, fixtureGUID3}
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
	activeGUIDs := []string{fixtureGUID2, fixtureGUID3}
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

	if !results[fixtureGUID1] {
		t.Errorf("item1 should be archived")
	}

	if results[fixtureGUID2] {
		t.Errorf("item2 should not be archived")
	}

	if results[fixtureGUID3] {
		t.Errorf("item3 should not be archived")
	}
}

func TestDeleteArchivedItems(t *testing.T) {
	db := setupTestDB(t)

	// Insert feed
	feed := &Feed{
		URL:      fixtureFeedURL,
		Title:    fixtureFeedTitle,
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

	// Should have 2 items (archived flag no longer filters items in queries)
	if len(allItems) != 2 {
		t.Errorf("Found %d items, want 2", len(allItems))
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

func TestGetItemsWithFeedAndTitleSubstringFilters(t *testing.T) {
	db := setupTestDB(t)
	discoveredAt := time.Date(2026, 8, 25, 14, 0, 0, 0, time.FixedZone("PDT", -7*60*60))
	feeds := []string{
		testAlphaFeedURL,
		testBetaFeedURL,
	}
	for _, feedURL := range feeds {
		if err := db.UpsertFeed(&Feed{URL: feedURL}); err != nil {
			t.Fatal(err)
		}
	}
	items := []*Item{
		{
			FeedURL: feeds[0], GUID: testAlphaGUID, Title: "Practical Go Patterns",
			PublishedDate: discoveredAt.UTC(),
			FirstSeen:     sql.NullTime{Time: discoveredAt, Valid: true},
		},
		{FeedURL: feeds[0], GUID: "alpha-rust", Title: "Rust Notes"},
		{FeedURL: feeds[1], GUID: "beta-go", Title: "Go Release"},
	}
	for _, item := range items {
		if err := db.UpsertItem(item); err != nil {
			t.Fatal(err)
		}
	}

	got, err := db.GetItems(&ItemFilter{FeedQuery: "ALPHA.EXAMPLE", Search: "go"})
	if err != nil {
		t.Fatalf("GetItems() error = %v", err)
	}
	if len(got) != 1 || got[0].GUID != testAlphaGUID {
		t.Errorf("GetItems() = %#v, want alpha-go", got)
	}

	newlyDiscovered, err := db.GetItems(&ItemFilter{
		Since: discoveredAt.UTC().Add(-time.Minute),
		Until: discoveredAt.UTC(),
	})
	if err != nil {
		t.Fatalf("discovery-time GetItems() error = %v", err)
	}
	if len(newlyDiscovered) != 1 || newlyDiscovered[0].GUID != testAlphaGUID {
		t.Errorf("discovery-time GetItems() = %#v, want back-dated alpha-go", newlyDiscovered)
	}
	atCursor, err := db.GetItems(&ItemFilter{Since: discoveredAt.UTC().Add(time.Second)})
	if err != nil {
		t.Fatalf("boundary GetItems() error = %v", err)
	}
	if len(atCursor) != 0 {
		t.Errorf("boundary GetItems() returned %d items, want half-open cursor window", len(atCursor))
	}

	literal, err := db.GetItems(&ItemFilter{Search: "%"})
	if err != nil {
		t.Fatalf("literal GetItems() error = %v", err)
	}
	if len(literal) != 0 {
		t.Errorf("literal GetItems() returned %d wildcard matches, want 0", len(literal))
	}
}

func TestGetItemsTimeWindowUsesIndex(t *testing.T) {
	db := setupTestDB(t)
	query, args := buildItemsQuery(&ItemFilter{
		Since: time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC),
		Until: time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC),
	})

	rows, err := db.conn.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var plan strings.Builder
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(detail)
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), "idx_items_effective_date") {
		t.Fatalf("time query plan does not use effective date index:\n%s", plan.String())
	}
}

func TestGetItemsEffectiveDateOrderingUsesIndex(t *testing.T) {
	db := setupTestDB(t)
	rows, err := db.conn.Query(`
		EXPLAIN QUERY PLAN
		SELECT id FROM items
		WHERE feed_url = ?
		ORDER BY julianday(COALESCE(published_date, first_seen)) DESC
	`, fixtureFeedURL)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var plan strings.Builder
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(detail)
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), "idx_items_feed_effective_date") || strings.Contains(plan.String(), "USE TEMP B-TREE") {
		t.Fatalf("per-feed effective-date query plan does not use composite index:\n%s", plan.String())
	}
}

// newUpsertItemTextFixture builds an item with markup in its content, ready
// to exercise the item_text derivation UpsertItem performs.
func newUpsertItemTextFixture(term string) *Item {
	return &Item{
		FeedURL:       fixtureFeedURL,
		GUID:          fixtureGUID,
		Title:         testItemTitle,
		Link:          fixtureItemLink,
		PublishedDate: time.Now().UTC().Truncate(time.Second),
		Content:       seededItemContent(0, term),
		Summary:       fixtureItemSummary,
	}
}

// setupItemTextFixtureDB seeds a feed so UpsertItem's foreign key succeeds.
func setupItemTextFixtureDB(t *testing.T) *DB {
	t.Helper()
	db := setupTestDB(t)
	if err := db.UpsertFeed(&Feed{URL: fixtureFeedURL}); err != nil {
		t.Fatal(err)
	}
	return db
}

// itemTextBody reads the derived body text for an item.
func itemTextBody(t *testing.T, db *DB, itemID int64) string {
	t.Helper()
	var body string
	if err := db.conn.QueryRow(
		`SELECT body FROM item_text WHERE item_id = ?`, itemID,
	).Scan(&body); err != nil {
		t.Fatalf("reading item_text body for item %d: %v", itemID, err)
	}
	return body
}

func TestUpsertItemIndexesNewItem(t *testing.T) {
	db := setupItemTextFixtureDB(t)
	item := newUpsertItemTextFixture(seedBodyTerm)

	if err := db.UpsertItem(item); err != nil {
		t.Fatalf("db.UpsertItem() error = %v", err)
	}

	stored, err := db.GetItem(item.FeedURL, item.GUID)
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil {
		t.Fatal("db.GetItem() returned nil after upsert")
	}

	want := itemtext.Derive(item.Title, item.Summary, item.Content, itemtext.DefaultOptions()).Body
	if got := itemTextBody(t, db, stored.ID); got != want {
		t.Errorf("item_text body = %q, want %q", got, want)
	}

	if got := countMatching(t, db, seedBodyTerm); got != 1 {
		t.Errorf("body term matches %d items, want 1", got)
	}
	integrityCheck(t, db)
}

func TestUpsertItemSkipsDerivationWhenUnchanged(t *testing.T) {
	db := setupItemTextFixtureDB(t)
	item := newUpsertItemTextFixture(seedBodyTerm)

	if err := db.UpsertItem(item); err != nil {
		t.Fatalf("first db.UpsertItem() error = %v", err)
	}
	stored, err := db.GetItem(item.FeedURL, item.GUID)
	if err != nil {
		t.Fatal(err)
	}
	before, ok := itemTextComputedAt(t, db)[stored.ID]
	if !ok {
		t.Fatal("no item_text row after first upsert")
	}

	if err := db.UpsertItem(item); err != nil {
		t.Fatalf("second db.UpsertItem() error = %v", err)
	}
	after, ok := itemTextComputedAt(t, db)[stored.ID]
	if !ok {
		t.Fatal("item_text row disappeared after second upsert")
	}

	if after != before {
		t.Errorf("computed_at moved from %q to %q; unchanged content should skip derivation", before, after)
	}
}

func TestUpsertItemReindexesChangedContent(t *testing.T) {
	db := setupItemTextFixtureDB(t)
	item := newUpsertItemTextFixture(seedBodyTerm)

	if err := db.UpsertItem(item); err != nil {
		t.Fatalf("first db.UpsertItem() error = %v", err)
	}
	if got := countMatching(t, db, seedBodyTerm); got != 1 {
		t.Fatalf("initial body term matches %d items, want 1", got)
	}

	item.Content = seededItemContent(0, replacementBodyTerm)
	if err := db.UpsertItem(item); err != nil {
		t.Fatalf("second db.UpsertItem() error = %v", err)
	}

	if got := countMatching(t, db, seedBodyTerm); got != 0 {
		t.Errorf("retired body term still matches %d items, want 0", got)
	}
	if got := countMatching(t, db, replacementBodyTerm); got != 1 {
		t.Errorf("new body term matches %d items, want 1", got)
	}
	integrityCheck(t, db)
}

func TestUpsertItemReindexesOnVersionBump(t *testing.T) {
	db := setupItemTextFixtureDB(t)
	item := newUpsertItemTextFixture(seedBodyTerm)

	if err := db.UpsertItem(item); err != nil {
		t.Fatalf("first db.UpsertItem() error = %v", err)
	}
	stored, err := db.GetItem(item.FeedURL, item.GUID)
	if err != nil {
		t.Fatal(err)
	}

	execSQL(t, db, `UPDATE item_text SET generator_version = ? WHERE item_id = ?`, itemtext.Version-1, stored.ID)
	before, ok := itemTextComputedAt(t, db)[stored.ID]
	if !ok {
		t.Fatal("no item_text row after hand-setting generator_version")
	}

	if err := db.UpsertItem(item); err != nil {
		t.Fatalf("second db.UpsertItem() error = %v", err)
	}

	after, ok := itemTextComputedAt(t, db)[stored.ID]
	if !ok {
		t.Fatal("item_text row disappeared after second upsert")
	}
	if after == before {
		t.Errorf("computed_at unchanged at %q; a version bump should force recompute despite a matching hash", before)
	}

	var version int
	if err := db.conn.QueryRow(
		`SELECT generator_version FROM item_text WHERE item_id = ?`, stored.ID,
	).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != itemtext.Version {
		t.Errorf("generator_version = %d, want %d", version, itemtext.Version)
	}
}
