package fetcher

import (
	"database/sql"
	"testing"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
)

func TestFetcher_isValidURL(t *testing.T) {
	// Create a test database and fetcher
	db, err := database.New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	if err := db.InitSchema(); err != nil {
		t.Fatalf("Failed to initialize test database: %v", err)
	}

	fetcher := NewFetcher(db, 30*time.Second, 100, false)

	tests := []struct {
		url      string
		expected bool
		name     string
	}{
		{"", false, "empty URL"},
		{"https://example.com", true, "valid HTTPS URL"},
		{"http://example.com", true, "valid HTTP URL"},
		{"https://example.com/path?query=value", true, "valid URL with path and query"},
		{"ftp://example.com", false, "FTP scheme not allowed"},
		{"javascript:alert('xss')", false, "JavaScript scheme not allowed"},
		{"https://", false, "missing host"},
		{"not-a-url", false, "invalid URL format"},
		{"https://example.com:8080/path", true, "valid URL with port"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := fetcher.isValidURL(test.url)
			if result != test.expected {
				t.Errorf("For URL '%s', expected %v, got %v", test.url, test.expected, result)
			}
		})
	}
}

func TestFetcher_filterURLsNeedingUnfurl(t *testing.T) {
	// Create a test database and fetcher
	db, err := database.New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	if err := db.InitSchema(); err != nil {
		t.Fatalf("Failed to initialize test database: %v", err)
	}

	fetcher := NewFetcher(db, 30*time.Second, 100, false)

	// Add metadata for one URL
	metadata := &database.URLMetadata{
		URL:             "https://example.com/existing",
		Title:           sql.NullString{String: "Existing", Valid: true},
		LastFetchAt:     sql.NullTime{Time: time.Now(), Valid: true},
		FetchStatusCode: sql.NullInt64{Int64: 200, Valid: true},
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	if err := db.UpsertMetadata(metadata); err != nil {
		t.Fatalf("Failed to upsert metadata: %v", err)
	}

	urls := []string{
		"https://example.com/existing",
		"https://example.com/new1",
		"https://example.com/new2",
	}

	filtered, err := fetcher.filterURLsNeedingUnfurl(urls)
	if err != nil {
		t.Fatalf("filterURLsNeedingUnfurl failed: %v", err)
	}

	// Should only return the URLs that don't have existing metadata
	expected := []string{
		"https://example.com/new1",
		"https://example.com/new2",
	}

	if len(filtered) != len(expected) {
		t.Errorf("Expected %d filtered URLs, got %d", len(expected), len(filtered))
	}

	for i, expectedURL := range expected {
		if i >= len(filtered) || filtered[i] != expectedURL {
			t.Errorf("Expected filtered URL[%d] = %s, got %s", i, expectedURL, filtered[i])
		}
	}
}

func TestFetcher_filterURLsNeedingUnfurl_Empty(t *testing.T) {
	// Create a test database and fetcher
	db, err := database.New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	if err := db.InitSchema(); err != nil {
		t.Fatalf("Failed to initialize test database: %v", err)
	}

	fetcher := NewFetcher(db, 30*time.Second, 100, false)

	// Test empty slice
	filtered, err := fetcher.filterURLsNeedingUnfurl([]string{})
	if err != nil {
		t.Fatalf("filterURLsNeedingUnfurl failed on empty slice: %v", err)
	}

	if len(filtered) != 0 {
		t.Errorf("Expected empty filtered slice, got %d items", len(filtered))
	}
}
