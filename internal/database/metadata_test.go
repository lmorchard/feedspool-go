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
		URL:              testURL,
		Title:            sql.NullString{String: "Test Page", Valid: true},
		Description:      sql.NullString{String: "A test page", Valid: true},
		LastFetchAt:      sql.NullTime{Time: time.Now(), Valid: true},
		FetchStatusCode:  sql.NullInt64{Int64: 200, Valid: true},
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
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
		URL:              urls[0],
		Title:            sql.NullString{String: "Exists", Valid: true},
		LastFetchAt:      sql.NullTime{Time: time.Now(), Valid: true},
		FetchStatusCode:  sql.NullInt64{Int64: 200, Valid: true},
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	metadata2 := &URLMetadata{
		URL:              urls[2],
		Title:            sql.NullString{String: "Also Exists", Valid: true},
		LastFetchAt:      sql.NullTime{Time: time.Now(), Valid: true},
		FetchStatusCode:  sql.NullInt64{Int64: 200, Valid: true},
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
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