package database

import (
	"database/sql"
	"testing"
	"time"
)

func TestHasUnfurlMetadata(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	if err := db.InitSchema(); err != nil {
		t.Fatalf("Failed to initialize test database: %v", err)
	}

	testURL := "https://example.com/test"

	// Test that URL doesn't exist initially
	exists, err := db.HasUnfurlMetadata(testURL)
	if err != nil {
		t.Fatalf("HasUnfurlMetadata failed: %v", err)
	}
	if exists {
		t.Error("Expected URL to not have metadata initially")
	}

	// Add some metadata
	metadata := &URLMetadata{
		URL:             testURL,
		Title:           sql.NullString{String: "Test Page", Valid: true},
		Description:     sql.NullString{String: "A test page", Valid: true},
		LastFetchAt:     sql.NullTime{Time: time.Now(), Valid: true},
		FetchStatusCode: sql.NullInt64{Int64: 200, Valid: true},
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	if err := db.UpsertMetadata(metadata); err != nil {
		t.Fatalf("Failed to upsert metadata: %v", err)
	}

	// Test that URL now exists
	exists, err = db.HasUnfurlMetadata(testURL)
	if err != nil {
		t.Fatalf("HasUnfurlMetadata failed: %v", err)
	}
	if !exists {
		t.Error("Expected URL to have metadata after upsert")
	}
}

func TestHasUnfurlMetadataBatch(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	if err := db.InitSchema(); err != nil {
		t.Fatalf("Failed to initialize test database: %v", err)
	}

	urls := []string{
		"https://example.com/exists",
		"https://example.com/not-exists",
		"https://example.com/also-exists",
	}

	// Add metadata for some URLs
	metadata1 := &URLMetadata{
		URL:             urls[0],
		Title:           sql.NullString{String: "Exists", Valid: true},
		LastFetchAt:     sql.NullTime{Time: time.Now(), Valid: true},
		FetchStatusCode: sql.NullInt64{Int64: 200, Valid: true},
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	metadata2 := &URLMetadata{
		URL:             urls[2],
		Title:           sql.NullString{String: "Also Exists", Valid: true},
		LastFetchAt:     sql.NullTime{Time: time.Now(), Valid: true},
		FetchStatusCode: sql.NullInt64{Int64: 200, Valid: true},
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	if err := db.UpsertMetadata(metadata1); err != nil {
		t.Fatalf("Failed to upsert metadata1: %v", err)
	}

	if err := db.UpsertMetadata(metadata2); err != nil {
		t.Fatalf("Failed to upsert metadata2: %v", err)
	}

	// Test batch check
	results, err := db.HasUnfurlMetadataBatch(urls)
	if err != nil {
		t.Fatalf("HasUnfurlMetadataBatch failed: %v", err)
	}

	expected := map[string]bool{
		urls[0]: true,  // exists
		urls[1]: false, // not-exists
		urls[2]: true,  // also-exists
	}

	for url, expectedExists := range expected {
		if results[url] != expectedExists {
			t.Errorf("For URL %s, expected %v, got %v", url, expectedExists, results[url])
		}
	}
}

func TestHasUnfurlMetadataBatch_Empty(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	if err := db.InitSchema(); err != nil {
		t.Fatalf("Failed to initialize test database: %v", err)
	}

	// Test empty slice
	results, err := db.HasUnfurlMetadataBatch([]string{})
	if err != nil {
		t.Fatalf("HasUnfurlMetadataBatch failed on empty slice: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("Expected empty result map, got %d entries", len(results))
	}
}

// Fixture URLs for TestGetURLsNeedingFetch, hoisted so goconst stays quiet
// about the repeated literals between seeding and assertions.
const (
	needsFetchURLNoMetadata     = "https://needs-fetch.example.com/no-metadata"
	needsFetchURLStaleFailure   = "https://needs-fetch.example.com/stale-failure"
	needsFetchURLSuccess        = "https://needs-fetch.example.com/success"
	needsFetchURLRecentFailure  = "https://needs-fetch.example.com/recent-failure"
	needsFetchURLArchived       = "https://needs-fetch.example.com/archived"
	needsFetchRetryAfter        = time.Hour
	needsFetchStatusCodeFailure = 500
	needsFetchStatusCodeSuccess = 200
)

// seedGetURLsNeedingFetchFixture builds a feed with items that exercise every
// branch of the GetURLsNeedingFetch predicate:
//   - a link with no url_metadata row at all (needs fetch)
//   - a link with a failed fetch whose last_fetch_at is older than the retry
//     cutoff (needs fetch, and published more recently than the item above so
//     ordering is unambiguous)
//   - a link with a successful fetch (must never need fetch, regardless of
//     how old last_fetch_at is)
//   - a link with a failed fetch that was attempted too recently to retry yet
//   - an archived item with no metadata (excluded by the archived filter,
//     even though the "no metadata" branch alone would otherwise match it)
//
// It returns the two URLs that should need fetching, in the order
// GetURLsNeedingFetch is expected to return them (published_date DESC).
func seedGetURLsNeedingFetchFixture(t *testing.T) (db *DB, wantNewestFirst []string) {
	t.Helper()

	db = setupTestDB(t)

	if err := db.UpsertFeed(&Feed{URL: fixtureFeedURL, Title: fixtureFeedTitle}); err != nil {
		t.Fatalf("UpsertFeed() error = %v", err)
	}

	// Local time, not UTC: GetURLsNeedingFetch computes its own retryTime
	// cutoff via a bare time.Now() (see metadata_repository.go), and SQLite
	// compares DATETIME columns as text. Seeding last_fetch_at in a
	// different UTC offset than that cutoff would make the lexicographic
	// comparison meaningless regardless of which instant is actually
	// earlier, so this must match production's own convention.
	now := time.Now()

	items := []*Item{
		{
			FeedURL: fixtureFeedURL, GUID: "no-metadata", Link: needsFetchURLNoMetadata,
			PublishedDate: now,
		},
		{
			FeedURL: fixtureFeedURL, GUID: "stale-failure", Link: needsFetchURLStaleFailure,
			PublishedDate: now.Add(-3 * time.Hour),
		},
		{
			FeedURL: fixtureFeedURL, GUID: "success", Link: needsFetchURLSuccess,
			PublishedDate: now,
		},
		{
			FeedURL: fixtureFeedURL, GUID: "recent-failure", Link: needsFetchURLRecentFailure,
			PublishedDate: now,
		},
		{
			FeedURL: fixtureFeedURL, GUID: "archived", Link: needsFetchURLArchived,
			PublishedDate: now, Archived: true,
		},
	}
	for _, item := range items {
		if err := db.UpsertItem(item); err != nil {
			t.Fatalf("UpsertItem(%s) error = %v", item.Link, err)
		}
	}

	metadata := []*URLMetadata{
		{
			// Failed fetch, attempted well before the retry cutoff: eligible.
			URL:             needsFetchURLStaleFailure,
			FetchStatusCode: sql.NullInt64{Int64: needsFetchStatusCodeFailure, Valid: true},
			LastFetchAt:     sql.NullTime{Time: now.Add(-2 * needsFetchRetryAfter), Valid: true},
		},
		{
			// Successful fetch: never eligible, no matter how stale.
			URL:             needsFetchURLSuccess,
			FetchStatusCode: sql.NullInt64{Int64: needsFetchStatusCodeSuccess, Valid: true},
			LastFetchAt:     sql.NullTime{Time: now.Add(-2 * needsFetchRetryAfter), Valid: true},
		},
		{
			// Failed fetch, but attempted too recently to retry yet.
			URL:             needsFetchURLRecentFailure,
			FetchStatusCode: sql.NullInt64{Int64: needsFetchStatusCodeFailure, Valid: true},
			LastFetchAt:     sql.NullTime{Time: now.Add(-needsFetchRetryAfter / 2), Valid: true},
		},
	}
	for _, m := range metadata {
		if err := db.UpsertMetadata(m); err != nil {
			t.Fatalf("UpsertMetadata(%s) error = %v", m.URL, err)
		}
	}

	// needsFetchURLNoMetadata and needsFetchURLStaleFailure are the only two
	// eligible URLs. needsFetchURLNoMetadata was published at "now", after
	// needsFetchURLStaleFailure at now-3h, so it sorts first under the
	// query's ORDER BY published_date DESC.
	return db, []string{needsFetchURLNoMetadata, needsFetchURLStaleFailure}
}

// TestGetURLsNeedingFetch exercises the LIMIT binding that this branch
// changed from an interpolated fmt.Sprintf("%d") into a bound "?" parameter
// appended to the args slice. A limit smaller than the number of eligible
// rows is the case that actually proves the binding (and its position in the
// args slice) is correct: get it wrong and the query either errors out or
// returns the wrong rows entirely, rather than merely returning too many.
func TestGetURLsNeedingFetch(t *testing.T) {
	db, wantNewestFirst := seedGetURLsNeedingFetchFixture(t)

	t.Run("no limit returns every eligible URL", func(t *testing.T) {
		got, err := db.GetURLsNeedingFetch(0, needsFetchRetryAfter)
		if err != nil {
			t.Fatalf("GetURLsNeedingFetch() error = %v", err)
		}
		assertStringSliceEqual(t, got, wantNewestFirst)
	})

	t.Run("limit larger than eligible count returns every eligible URL", func(t *testing.T) {
		got, err := db.GetURLsNeedingFetch(len(wantNewestFirst)+10, needsFetchRetryAfter)
		if err != nil {
			t.Fatalf("GetURLsNeedingFetch() error = %v", err)
		}
		assertStringSliceEqual(t, got, wantNewestFirst)
	})

	t.Run("limit smaller than eligible count truncates to the newest", func(t *testing.T) {
		got, err := db.GetURLsNeedingFetch(1, needsFetchRetryAfter)
		if err != nil {
			t.Fatalf("GetURLsNeedingFetch() error = %v", err)
		}
		assertStringSliceEqual(t, got, wantNewestFirst[:1])
	})
}

// assertStringSliceEqual fails t if got and want differ in length or order.
func assertStringSliceEqual(t *testing.T, got, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q (got %v, want %v)", i, got[i], want[i], got, want)
		}
	}
}
