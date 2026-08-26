package database

import (
	"database/sql"
	"testing"
)

const annotationTestFeed = "https://example.com/feed.xml"

func seedFeedAndItem(t *testing.T, db *DB, feedURL, guid string) {
	t.Helper()
	if err := db.UpsertFeed(&Feed{URL: feedURL, Title: "Test"}); err != nil {
		t.Fatalf("UpsertFeed() error = %v", err)
	}
	if err := db.UpsertItem(&Item{FeedURL: feedURL, GUID: guid, Title: "Item"}); err != nil {
		t.Fatalf("UpsertItem() error = %v", err)
	}
}

// Repeating an annotation is the common case over HTTP -- a UI marking items
// seen on scroll, or an agent re-running its triage -- so it has to be a no-op
// rather than an append.
func TestAddAnnotationIsIdempotent(t *testing.T) {
	db := setupTestDB(t)
	seedFeedAndItem(t, db, annotationTestFeed, "guid-1")

	for range 3 {
		if err := db.AddAnnotation(
			annotationTestFeed, "guid-1", "seen", sql.NullString{}, sql.NullString{},
		); err != nil {
			t.Fatalf("AddAnnotation() error = %v", err)
		}
	}

	annotations, err := db.GetAnnotations(annotationTestFeed, "guid-1")
	if err != nil {
		t.Fatalf("GetAnnotations() error = %v", err)
	}
	if len(annotations) != 1 {
		t.Errorf("annotations after 3 identical adds = %d, want 1", len(annotations))
	}
}

func TestAddAnnotationDistinctValuesCoexist(t *testing.T) {
	db := setupTestDB(t)
	seedFeedAndItem(t, db, annotationTestFeed, "guid-1")

	for _, value := range []string{annotationValueLater, annotationValueUrgent} {
		if err := db.AddAnnotation(annotationTestFeed, "guid-1", annotationKindTag,
			sql.NullString{String: value, Valid: true}, sql.NullString{}); err != nil {
			t.Fatalf("AddAnnotation(%q) error = %v", value, err)
		}
	}

	annotations, err := db.GetAnnotations(annotationTestFeed, "guid-1")
	if err != nil {
		t.Fatalf("GetAnnotations() error = %v", err)
	}
	if len(annotations) != 2 {
		t.Errorf("annotations = %d, want 2 distinct values to coexist", len(annotations))
	}
}

// A NULL value and a set value are different annotations, and the COALESCE in
// the unique index must not collapse them into one.
func TestAddAnnotationNullAndValuedCoexist(t *testing.T) {
	db := setupTestDB(t)
	seedFeedAndItem(t, db, annotationTestFeed, "guid-1")

	if err := db.AddAnnotation(annotationTestFeed, "guid-1", annotationKindTag,
		sql.NullString{}, sql.NullString{}); err != nil {
		t.Fatal(err)
	}
	if err := db.AddAnnotation(annotationTestFeed, "guid-1", annotationKindTag,
		sql.NullString{String: annotationValueLater, Valid: true}, sql.NullString{}); err != nil {
		t.Fatal(err)
	}

	annotations, err := db.GetAnnotations(annotationTestFeed, "guid-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(annotations) != 2 {
		t.Errorf("annotations = %d, want 2 (NULL and 'later' are distinct)", len(annotations))
	}
}

func TestAnnotationExists(t *testing.T) {
	db := setupTestDB(t)
	seedFeedAndItem(t, db, annotationTestFeed, "guid-1")

	exists, err := db.AnnotationExists(annotationTestFeed, "guid-1", "seen", sql.NullString{})
	if err != nil {
		t.Fatalf("AnnotationExists() error = %v", err)
	}
	if exists {
		t.Error("AnnotationExists() = true before any add, want false")
	}

	if err := db.AddAnnotation(annotationTestFeed, "guid-1", "seen",
		sql.NullString{}, sql.NullString{}); err != nil {
		t.Fatal(err)
	}

	exists, err = db.AnnotationExists(annotationTestFeed, "guid-1", "seen", sql.NullString{})
	if err != nil {
		t.Fatalf("AnnotationExists() error = %v", err)
	}
	if !exists {
		t.Error("AnnotationExists() = false after add, want true")
	}
}

func TestAnnotationExistsDistinguishesValues(t *testing.T) {
	db := setupTestDB(t)
	seedFeedAndItem(t, db, annotationTestFeed, "guid-1")

	if err := db.AddAnnotation(annotationTestFeed, "guid-1", annotationKindTag,
		sql.NullString{String: annotationValueLater, Valid: true}, sql.NullString{}); err != nil {
		t.Fatal(err)
	}

	exists, err := db.AnnotationExists(annotationTestFeed, "guid-1", annotationKindTag,
		sql.NullString{String: annotationValueUrgent, Valid: true})
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Error("AnnotationExists() = true for a value that was never added")
	}
}
