package database

import (
	"testing"
	"time"
)

const (
	testModelNomic = "nomic-embed-text"
	testModelQwen3 = "qwen3-embedding:0.6b"
	testHashA      = "hash-a"
	testHashB      = "hash-b"
)

// unitVectorAt returns a one-hot unit vector, so stored fixtures satisfy the
// same normalization the real providers do.
func unitVectorAt(dims, hot int) []float32 {
	v := make([]float32, dims)
	v[hot] = 1
	return v
}

// seedItem inserts a feed and one item, returning the item's row id.
func seedItem(t *testing.T, db *DB, guid string, published time.Time) int64 {
	t.Helper()

	if err := db.UpsertFeed(&Feed{
		URL:      fixtureFeedURL,
		Title:    fixtureFeedTitle,
		FeedJSON: JSON(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	item := &Item{
		FeedURL:       fixtureFeedURL,
		GUID:          guid,
		Title:         "Item " + guid,
		Link:          "https://example.com/" + guid,
		PublishedDate: published,
		Content:       "content for " + guid,
		Summary:       "summary for " + guid,
		ItemJSON:      JSON(`{}`),
	}
	if err := db.UpsertItem(item); err != nil {
		t.Fatal(err)
	}

	var id int64
	if err := db.conn.QueryRow(
		`SELECT id FROM items WHERE feed_url = ? AND guid = ?`, fixtureFeedURL, guid,
	).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// putEmbedding writes one embedding through a transaction, the way the
// backfill generator will.
func putEmbedding(t *testing.T, db *DB, itemID int64, modelID string, vector []float32, hash string) {
	t.Helper()

	tx, err := db.conn.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := upsertItemEmbeddingTx(tx, itemID, modelID, vector, hash); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestItemEmbeddingRoundTrip(t *testing.T) {
	db := setupTestDB(t)
	id := seedItem(t, db, "item-1", time.Now().UTC())

	want := unitVectorAt(768, 3)
	putEmbedding(t, db, id, testModelNomic, want, testHashA)

	got, err := db.GetItemEmbedding(id, testModelNomic)
	if err != nil {
		t.Fatalf("GetItemEmbedding: %v", err)
	}
	if got.Dims != 768 {
		t.Errorf("Dims = %d, want 768", got.Dims)
	}
	if got.SourceHash != testHashA {
		t.Errorf("SourceHash = %q, want %q", got.SourceHash, testHashA)
	}
	if len(got.Vector) != 768 || got.Vector[3] != 1 {
		t.Errorf("vector did not survive the round trip: len=%d", len(got.Vector))
	}
	if got.ComputedAt.IsZero() {
		t.Error("ComputedAt is zero; the write path must stamp it")
	}
}

// The whole reason for the composite primary key: a bake-off needs both
// models' answers for the same item at the same time, and the two disagree on
// vector width.
func TestItemEmbeddingTwoModelsCoexist(t *testing.T) {
	db := setupTestDB(t)
	id := seedItem(t, db, "item-1", time.Now().UTC())

	putEmbedding(t, db, id, testModelNomic, unitVectorAt(768, 1), testHashA)
	putEmbedding(t, db, id, testModelQwen3, unitVectorAt(1024, 2), testHashA)

	nomic, err := db.GetItemEmbedding(id, testModelNomic)
	if err != nil {
		t.Fatalf("GetItemEmbedding(nomic): %v", err)
	}
	qwen, err := db.GetItemEmbedding(id, testModelQwen3)
	if err != nil {
		t.Fatalf("GetItemEmbedding(qwen3): %v", err)
	}

	if nomic.Dims != 768 {
		t.Errorf("nomic Dims = %d, want 768", nomic.Dims)
	}
	if qwen.Dims != 1024 {
		t.Errorf("qwen3 Dims = %d, want 1024", qwen.Dims)
	}
	if nomic.Vector[1] != 1 || qwen.Vector[2] != 1 {
		t.Error("the two models' vectors were not stored independently")
	}

	var rows int
	if err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM item_embeddings WHERE item_id = ?`, id,
	).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Errorf("item has %d embedding rows, want 2 (one per model)", rows)
	}
}

func TestItemEmbeddingUpsertReplacesRatherThanDuplicating(t *testing.T) {
	db := setupTestDB(t)
	id := seedItem(t, db, "item-1", time.Now().UTC())

	putEmbedding(t, db, id, testModelNomic, unitVectorAt(768, 1), testHashA)
	putEmbedding(t, db, id, testModelNomic, unitVectorAt(768, 7), testHashB)

	var rows int
	if err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM item_embeddings WHERE item_id = ? AND model_id = ?`,
		id, testModelNomic,
	).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("got %d rows for one (item, model) pair, want 1", rows)
	}

	got, err := db.GetItemEmbedding(id, testModelNomic)
	if err != nil {
		t.Fatal(err)
	}
	if got.Vector[7] != 1 || got.Vector[1] != 0 {
		t.Error("the re-upserted vector did not replace the original")
	}
	if got.SourceHash != testHashB {
		t.Errorf("SourceHash = %q, want the updated %q", got.SourceHash, testHashB)
	}
}

// ON DELETE CASCADE is what lets DeleteFeed and purge stay unaware of
// embeddings, exactly as they are unaware of item_text.
func TestItemEmbeddingCascadesOnItemDelete(t *testing.T) {
	db := setupTestDB(t)
	id := seedItem(t, db, "item-1", time.Now().UTC())
	putEmbedding(t, db, id, testModelNomic, unitVectorAt(768, 1), testHashA)

	if _, err := db.conn.Exec(`DELETE FROM items WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}

	var rows int
	if err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM item_embeddings WHERE item_id = ?`, id,
	).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Errorf("%d embedding rows survived the item delete, want 0", rows)
	}
}

func TestGetItemEmbeddingMissingIsDistinguishable(t *testing.T) {
	db := setupTestDB(t)
	id := seedItem(t, db, "item-1", time.Now().UTC())

	_, err := db.GetItemEmbedding(id, testModelNomic)
	if err == nil {
		t.Fatal("GetItemEmbedding on an unembedded item succeeded, want an error")
	}
	if !IsNoEmbedding(err) {
		t.Errorf("error %v is not reported as a missing embedding; callers cannot "+
			"tell 'not embedded yet' from a real failure", err)
	}
}

// A blob whose length disagrees with its dims column can only come from
// corruption, and must not decode into a plausible-looking vector.
func TestGetItemEmbeddingRejectsCorruptBlob(t *testing.T) {
	db := setupTestDB(t)
	id := seedItem(t, db, "item-1", time.Now().UTC())
	putEmbedding(t, db, id, testModelNomic, unitVectorAt(768, 1), testHashA)

	// Claim 1024 dims for a 768-dim blob.
	if _, err := db.conn.Exec(
		`UPDATE item_embeddings SET dims = 1024 WHERE item_id = ? AND model_id = ?`,
		id, testModelNomic,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := db.GetItemEmbedding(id, testModelNomic); err == nil {
		t.Fatal("a blob shorter than its dims column decoded successfully, want an error")
	}
}
