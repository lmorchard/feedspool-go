package database

import "testing"

const (
	annotationKindTag      = "tag"
	annotationValueLater   = "later"
	annotationValueUrgent  = "urgent"
	earliestAnnotationTime = "2026-01-01 00:00:00"
)

func TestMigration10DedupesAnnotationsKeepingEarliest(t *testing.T) {
	db := setupTestDB(t)
	seedFeedAndItem(t, db, annotationTestFeed, "guid-1")
	dropAnnotationUniqueIndex(t, db)

	// Inserted newest-first, so a naive "keep the last row" would fail here.
	for _, timestamp := range []string{"2026-02-02 00:00:00", earliestAnnotationTime} {
		if _, err := db.conn.Exec(
			`INSERT INTO item_annotations (feed_url, item_guid, kind, value, actor, created_at)
			 VALUES (?, ?, 'seen', NULL, NULL, ?)`,
			annotationTestFeed, "guid-1", timestamp,
		); err != nil {
			t.Fatal(err)
		}
	}

	runMigration10SQL(t, db)

	var count int
	var createdAt string
	if err := db.conn.QueryRow(
		`SELECT COUNT(*), MIN(created_at) FROM item_annotations WHERE kind = 'seen'`,
	).Scan(&count, &createdAt); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("seen annotations after migration = %d, want 1", count)
	}
	if createdAt != earliestAnnotationTime {
		t.Errorf("surviving created_at = %q, want the earliest sighting", createdAt)
	}
}

func TestMigration10KeepsDistinctValues(t *testing.T) {
	db := setupTestDB(t)
	seedFeedAndItem(t, db, annotationTestFeed, "guid-1")
	dropAnnotationUniqueIndex(t, db)

	for _, value := range []string{annotationValueLater, annotationValueUrgent} {
		if _, err := db.conn.Exec(
			`INSERT INTO item_annotations (feed_url, item_guid, kind, value)
			 VALUES (?, ?, 'tag', ?)`,
			annotationTestFeed, "guid-1", value,
		); err != nil {
			t.Fatal(err)
		}
	}

	runMigration10SQL(t, db)

	var count int
	if err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM item_annotations WHERE kind = 'tag'`,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("distinct-valued annotations = %d, want both kept", count)
	}
}

// A NULL value and a set value must survive the dedupe as separate rows --
// COALESCE in the unique index groups them only against their own kind.
func TestMigration10KeepsNullAndValuedSeparate(t *testing.T) {
	db := setupTestDB(t)
	seedFeedAndItem(t, db, annotationTestFeed, "guid-1")
	dropAnnotationUniqueIndex(t, db)

	if _, err := db.conn.Exec(
		`INSERT INTO item_annotations (feed_url, item_guid, kind, value)
		 VALUES (?, ?, 'tag', NULL), (?, ?, 'tag', 'later')`,
		annotationTestFeed, "guid-1", annotationTestFeed, "guid-1",
	); err != nil {
		t.Fatal(err)
	}

	runMigration10SQL(t, db)

	var count int
	if err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM item_annotations WHERE kind = 'tag'`,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("annotations after migration = %d, want NULL and 'later' both kept", count)
	}
}

// The dedupe is only a fix if the index it creates actually holds afterward;
// otherwise it is a one-time cleanup that decays back to duplicates.
func TestMigration10IndexPreventsReinsert(t *testing.T) {
	db := setupTestDB(t)
	seedFeedAndItem(t, db, annotationTestFeed, "guid-1")

	for range 2 {
		if _, err := db.conn.Exec(
			`INSERT INTO item_annotations (feed_url, item_guid, kind, value)
			 VALUES (?, ?, 'seen', NULL) ON CONFLICT DO NOTHING`,
			annotationTestFeed, "guid-1",
		); err != nil {
			t.Fatal(err)
		}
	}

	var count int
	if err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM item_annotations WHERE kind = 'seen'`,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("seen annotations after 2 raw inserts = %d, want 1", count)
	}
}

// dropAnnotationUniqueIndex reproduces the pre-migration state. setupTestDB
// runs InitSchema, which now creates the index up front.
func dropAnnotationUniqueIndex(t *testing.T, db *DB) {
	t.Helper()
	if _, err := db.conn.Exec(`DROP INDEX IF EXISTS idx_item_annotations_unique`); err != nil {
		t.Fatal(err)
	}
}

// runMigration10SQL applies migration 10's statements directly. InitSchema has
// already recorded the current schema version, so applyMigration10 would fail
// trying to record it a second time.
func runMigration10SQL(t *testing.T, db *DB) {
	t.Helper()
	if _, err := db.conn.Exec(getMigrations()[migrationVersion10]); err != nil {
		t.Fatalf("migration 10 SQL error = %v", err)
	}
}
