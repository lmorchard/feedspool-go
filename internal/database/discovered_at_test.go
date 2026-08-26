package database

import (
	"database/sql"
	"fmt"
	"testing"
	"time"
)

// DiscoveredAt and EffectiveDate deliberately use opposite precedence, and the
// pair is easy to conflate. DiscoveredAt has to match discoveryTimeExpression,
// which is what --since and --until compare against; EffectiveDate is the
// ordering key. A client polling with "since = max(discovered_at)" breaks if
// these get swapped.
func TestDiscoveredAtPrefersFirstSeen(t *testing.T) {
	published := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	firstSeen := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	item := &Item{
		PublishedDate: published,
		FirstSeen:     sql.NullTime{Time: firstSeen, Valid: true},
	}

	if got := item.DiscoveredAt(); !got.Equal(firstSeen) {
		t.Errorf("DiscoveredAt() = %v, want first_seen %v", got, firstSeen)
	}
	if got := item.EffectiveDate(); !got.Equal(published) {
		t.Errorf("EffectiveDate() = %v, want published_date %v", got, published)
	}
}

func TestDiscoveredAtFallsBackToPublished(t *testing.T) {
	published := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		item *Item
	}{
		{"first_seen absent", &Item{PublishedDate: published}},
		{
			"first_seen holds the zero-value sentinel",
			&Item{PublishedDate: published, FirstSeen: sql.NullTime{Time: time.Time{}, Valid: true}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.item.DiscoveredAt(); !got.Equal(published) {
				t.Errorf("DiscoveredAt() = %v, want %v", got, published)
			}
		})
	}
}

func TestDiscoveredAtZeroWhenNeitherIsSet(t *testing.T) {
	if got := (&Item{}).DiscoveredAt(); !got.IsZero() {
		t.Errorf("DiscoveredAt() = %v, want the zero time", got)
	}
}

// The Go helper must agree with the SQL expression the filter uses, or the
// discovered_at a client reads and the since it sends back drift apart.
func TestDiscoveredAtAgreesWithSQLFilter(t *testing.T) {
	db := setupTestDB(t)
	const feedURL = "https://example.com/feed.xml"
	if err := db.UpsertFeed(&Feed{URL: feedURL, Title: testFeedTitle}); err != nil {
		t.Fatal(err)
	}
	// Post-dated: published_date sits in the future relative to first_seen,
	// which is exactly where the two precedences disagree.
	if err := db.UpsertItem(&Item{
		FeedURL:       feedURL,
		GUID:          "postdated",
		Title:         "Post-dated",
		PublishedDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		FirstSeen:     sql.NullTime{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true},
	}); err != nil {
		t.Fatal(err)
	}

	stored, err := db.GetItemByHashID(ItemHashID(feedURL, "postdated"))
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil {
		t.Fatal("seeded item not found")
	}
	discovered := stored.DiscoveredAt()

	after, _, err := db.ListItems(&ItemPage{Limit: 10, Since: discovered.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Errorf("since=DiscoveredAt+1h returned %d items, want 0", len(after))
	}

	before, _, err := db.ListItems(&ItemPage{Limit: 10, Since: discovered.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 {
		t.Errorf("since=DiscoveredAt-1h returned %d items, want 1", len(before))
	}
}

// Since is exclusive and Until is inclusive, matching itemInDiscoveryWindow.
// Exclusive Since is what makes "poll with max(discovered_at)" return only new
// items instead of re-delivering the whole boundary batch every time.
func TestSinceIsExclusiveAndUntilIsInclusive(t *testing.T) {
	db := setupTestDB(t)
	const feedURL = "https://example.com/feed.xml"
	if err := db.UpsertFeed(&Feed{URL: feedURL, Title: testFeedTitle}); err != nil {
		t.Fatal(err)
	}
	// One fetch stamps every item with the same first_seen, which is the
	// common real-world case and exactly where the boundary matters.
	stamp := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	for i := range 3 {
		if err := db.UpsertItem(&Item{
			FeedURL:   feedURL,
			GUID:      fmt.Sprintf("guid-%d", i),
			Title:     fmt.Sprintf("Item %d", i),
			FirstSeen: sql.NullTime{Time: stamp, Valid: true},
		}); err != nil {
			t.Fatal(err)
		}
	}

	atBoundary, _, err := db.ListItems(&ItemPage{Limit: 10, Since: stamp})
	if err != nil {
		t.Fatal(err)
	}
	if len(atBoundary) != 0 {
		t.Errorf("since=exact timestamp returned %d items, want 0 (Since is exclusive)", len(atBoundary))
	}

	untilBoundary, _, err := db.ListItems(&ItemPage{Limit: 10, Until: stamp})
	if err != nil {
		t.Fatal(err)
	}
	if len(untilBoundary) != 3 {
		t.Errorf("until=exact timestamp returned %d items, want 3 (Until is inclusive)", len(untilBoundary))
	}
}
