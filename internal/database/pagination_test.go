package database

import (
	"database/sql"
	"fmt"
	"testing"
	"time"
)

const (
	paginationTestFeed = "https://example.com/feed.xml"
	testFeedTitle      = "Test"
	guidUndated        = "no-date"
	guidDated          = "dated"
	guidOhOne          = "guid-001"
)

func seedItems(t *testing.T, db *DB, feedURL string, count int) {
	t.Helper()
	if err := db.UpsertFeed(&Feed{URL: feedURL, Title: testFeedTitle}); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range count {
		if err := db.UpsertItem(&Item{
			FeedURL:       feedURL,
			GUID:          fmt.Sprintf("guid-%03d", i),
			Title:         fmt.Sprintf("Item %03d", i),
			Link:          fmt.Sprintf("https://example.com/%03d", i),
			PublishedDate: base.Add(time.Duration(i) * time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func drainItems(t *testing.T, db *DB, page *ItemPage) []*Item {
	t.Helper()
	var all []*Item
	var cursor *ItemCursor
	for iterations := 0; ; iterations++ {
		if iterations > 100 {
			t.Fatal("pagination did not terminate")
		}
		page.After = cursor
		items, next, err := db.ListItems(page)
		if err != nil {
			t.Fatalf("ListItems() error = %v", err)
		}
		all = append(all, items...)
		if next == nil {
			return all
		}
		cursor = next
	}
}

func TestListItemsPagesWithoutGapsOrDuplicates(t *testing.T) {
	db := setupTestDB(t)
	seedItems(t, db, paginationTestFeed, 25)

	seen := map[string]bool{}
	for _, item := range drainItems(t, db, &ItemPage{Limit: 10}) {
		if seen[item.GUID] {
			t.Fatalf("item %q returned twice", item.GUID)
		}
		seen[item.GUID] = true
	}
	if len(seen) != 25 {
		t.Errorf("distinct items paged = %d, want 25", len(seen))
	}
}

// The offset-pagination failure mode: a row arriving at the head of the
// ordering shifts every later page by one, duplicating a row across the seam.
// Keyset pagination must be immune.
func TestListItemsInsertionMidScanDoesNotDuplicate(t *testing.T) {
	db := setupTestDB(t)
	seedItems(t, db, paginationTestFeed, 20)

	first, cursor, err := db.ListItems(&ItemPage{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if cursor == nil {
		t.Fatal("expected a continuation cursor after the first page")
	}

	if err := db.UpsertItem(&Item{
		FeedURL:       paginationTestFeed,
		GUID:          "guid-999",
		Title:         "Breaking",
		PublishedDate: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}

	second, _, err := db.ListItems(&ItemPage{Limit: 10, After: cursor})
	if err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
	for _, item := range append(first, second...) {
		if seen[item.GUID] {
			t.Errorf("item %q appeared on both pages after a mid-scan insert", item.GUID)
		}
		seen[item.GUID] = true
	}
}

func TestListItemsNewestFirstByDefault(t *testing.T) {
	db := setupTestDB(t)
	seedItems(t, db, paginationTestFeed, 5)

	items, _, err := db.ListItems(&ItemPage{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if items[0].GUID != "guid-004" {
		t.Errorf("first item = %q, want the newest (guid-004)", items[0].GUID)
	}
}

func TestListItemsAscending(t *testing.T) {
	db := setupTestDB(t)
	seedItems(t, db, paginationTestFeed, 5)

	items := drainItems(t, db, &ItemPage{Limit: 2, Ascending: true})
	if len(items) != 5 {
		t.Fatalf("items = %d, want 5", len(items))
	}
	if items[0].GUID != "guid-000" {
		t.Errorf("first item = %q, want the oldest (guid-000)", items[0].GUID)
	}
	for i := 1; i < len(items); i++ {
		if items[i].EffectiveDate().Before(items[i-1].EffectiveDate()) {
			t.Fatalf("ascending order broken at index %d", i)
		}
	}
}

// Scraped items deliberately carry no published_date, so they must not
// disappear or scatter through the ordering.
func TestListItemsOrdersUndatedRowsLast(t *testing.T) {
	db := setupTestDB(t)
	if err := db.UpsertFeed(&Feed{URL: paginationTestFeed, Title: testFeedTitle}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertItem(&Item{FeedURL: paginationTestFeed, GUID: guidUndated, Title: "Undated"}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertItem(&Item{
		FeedURL: paginationTestFeed, GUID: guidDated, Title: "Dated",
		PublishedDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}

	items, _, err := db.ListItems(&ItemPage{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if items[0].GUID != guidDated || items[1].GUID != guidUndated {
		t.Errorf("order = [%q %q], want dated then undated", items[0].GUID, items[1].GUID)
	}
}

// Undated rows share a rank and a NULL date, so only the id can separate them.
// If the cursor mishandles that block, paging through it loops or skips.
func TestListItemsPagesThroughUndatedBlock(t *testing.T) {
	db := setupTestDB(t)
	if err := db.UpsertFeed(&Feed{URL: paginationTestFeed, Title: testFeedTitle}); err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		if err := db.UpsertItem(&Item{
			FeedURL: paginationTestFeed,
			GUID:    fmt.Sprintf("undated-%d", i),
			Title:   fmt.Sprintf("Undated %d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}

	seen := map[string]bool{}
	for _, item := range drainItems(t, db, &ItemPage{Limit: 2}) {
		if seen[item.GUID] {
			t.Fatalf("undated item %q returned twice", item.GUID)
		}
		seen[item.GUID] = true
	}
	if len(seen) != 5 {
		t.Errorf("undated items paged = %d, want 5", len(seen))
	}
}

func TestListItemsFiltersBySearch(t *testing.T) {
	db := setupTestDB(t)
	seedItems(t, db, paginationTestFeed, 5)

	// Uppercase input against a mixed-case title: the match is case-insensitive.
	items, _, err := db.ListItems(&ItemPage{Limit: 10, Search: "ITEM 003"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].GUID != "guid-003" {
		t.Errorf("search returned %d items, want exactly guid-003", len(items))
	}
}

func TestListItemsFiltersBySeen(t *testing.T) {
	db := setupTestDB(t)
	seedItems(t, db, paginationTestFeed, 3)
	if err := db.AddAnnotation(paginationTestFeed, "guid-001", "seen",
		sql.NullString{}, sql.NullString{}); err != nil {
		t.Fatal(err)
	}

	seenTrue := true
	items, _, err := db.ListItems(&ItemPage{Limit: 10, Seen: &seenTrue})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].GUID != guidOhOne {
		t.Errorf("seen=true returned %d items, want exactly guid-001", len(items))
	}

	seenFalse := false
	items, _, err = db.ListItems(&ItemPage{Limit: 10, Seen: &seenFalse})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Errorf("seen=false returned %d items, want 2", len(items))
	}
}

func TestListItemsFiltersByLink(t *testing.T) {
	db := setupTestDB(t)
	seedItems(t, db, paginationTestFeed, 5)

	items, _, err := db.ListItems(&ItemPage{Limit: 10, Link: "https://example.com/002"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].GUID != "guid-002" {
		t.Errorf("link filter returned %d items, want exactly guid-002", len(items))
	}
}

func TestListFeedsPagesWithoutGapsOrDuplicates(t *testing.T) {
	db := setupTestDB(t)
	for i := range 7 {
		if err := db.UpsertFeed(&Feed{
			URL:   fmt.Sprintf("https://example.com/%02d.xml", i),
			Title: fmt.Sprintf("Feed %02d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}

	seen := map[string]bool{}
	after := ""
	for iterations := 0; ; iterations++ {
		if iterations > 50 {
			t.Fatal("feed pagination did not terminate")
		}
		feeds, next, err := db.ListFeeds(&FeedPage{Limit: 3, After: after})
		if err != nil {
			t.Fatal(err)
		}
		for _, feed := range feeds {
			if seen[feed.URL] {
				t.Fatalf("feed %q returned twice", feed.URL)
			}
			seen[feed.URL] = true
		}
		if next == "" {
			break
		}
		after = next
	}
	if len(seen) != 7 {
		t.Errorf("feeds paged = %d, want 7", len(seen))
	}
}

func TestListFeedsCarriesParserColumns(t *testing.T) {
	db := setupTestDB(t)
	if err := db.UpsertFeed(&Feed{URL: paginationTestFeed, Title: testFeedTitle}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetFeedParserConfig(paginationTestFeed, FeedTypeScrape, "article h2 a"); err != nil {
		t.Fatal(err)
	}

	feeds, _, err := db.ListFeeds(&FeedPage{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 1 {
		t.Fatalf("feeds = %d, want 1", len(feeds))
	}
	if feeds[0].Type != FeedTypeScrape {
		t.Errorf("Type = %q, want %q", feeds[0].Type, FeedTypeScrape)
	}
	if feeds[0].ScrapeSelector != "article h2 a" {
		t.Errorf("ScrapeSelector = %q, want the configured selector", feeds[0].ScrapeSelector)
	}
}

func TestGetItemByHashID(t *testing.T) {
	db := setupTestDB(t)
	seedItems(t, db, paginationTestFeed, 3)

	item, err := db.GetItemByHashID(ItemHashID(paginationTestFeed, guidOhOne))
	if err != nil {
		t.Fatal(err)
	}
	if item == nil || item.GUID != "guid-001" {
		t.Fatalf("GetItemByHashID() = %v, want guid-001", item)
	}
}

func TestGetItemByHashIDUnknownReturnsNil(t *testing.T) {
	db := setupTestDB(t)
	seedItems(t, db, paginationTestFeed, 3)

	item, err := db.GetItemByHashID("ffffffffffffffff")
	if err != nil {
		t.Fatal(err)
	}
	if item != nil {
		t.Errorf("GetItemByHashID(unknown) = %v, want nil", item)
	}
}

func TestGetFeedByHashID(t *testing.T) {
	db := setupTestDB(t)
	seedItems(t, db, paginationTestFeed, 1)

	feed, err := db.GetFeedByHashID(FeedHashID(paginationTestFeed))
	if err != nil {
		t.Fatal(err)
	}
	if feed == nil || feed.URL != paginationTestFeed {
		t.Fatalf("GetFeedByHashID() = %v, want the seeded feed", feed)
	}

	missing, err := db.GetFeedByHashID("ffffffff")
	if err != nil {
		t.Fatal(err)
	}
	if missing != nil {
		t.Errorf("GetFeedByHashID(unknown) = %v, want nil", missing)
	}
}

func TestCountItemsForFeed(t *testing.T) {
	db := setupTestDB(t)
	seedItems(t, db, paginationTestFeed, 4)
	if err := db.AddAnnotation(paginationTestFeed, "guid-000", "seen",
		sql.NullString{}, sql.NullString{}); err != nil {
		t.Fatal(err)
	}

	total, unseen, err := db.CountItemsForFeed(paginationTestFeed)
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 {
		t.Errorf("total = %d, want 4", total)
	}
	if unseen != 3 {
		t.Errorf("unseen = %d, want 3", unseen)
	}
}

// SUM over an empty set is NULL in SQLite, which would fail the scan into an
// int if the query did not COALESCE it.
func TestCountItemsForFeedWithNoItems(t *testing.T) {
	db := setupTestDB(t)
	if err := db.UpsertFeed(&Feed{URL: paginationTestFeed, Title: "Empty"}); err != nil {
		t.Fatal(err)
	}

	total, unseen, err := db.CountItemsForFeed(paginationTestFeed)
	if err != nil {
		t.Fatalf("CountItemsForFeed() error = %v", err)
	}
	if total != 0 || unseen != 0 {
		t.Errorf("counts = (%d, %d), want (0, 0)", total, unseen)
	}
}
