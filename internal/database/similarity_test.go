package database

import (
	"math"
	"testing"
	"time"
)

// unitVectorFrom normalizes the given components so fixtures satisfy the same
// unit-length property the real providers guarantee.
func unitVectorFrom(dims int, components map[int]float32) []float32 {
	v := make([]float32, dims)
	var sum float64
	for i, c := range components {
		v[i] = c
		sum += float64(c) * float64(c)
	}
	norm := float32(math.Sqrt(sum))
	for i := range v {
		v[i] /= norm
	}
	return v
}

func TestNearestItemsRanksByDotProduct(t *testing.T) {
	db := setupTestDB(t)

	const dims = 8
	subject := seedItem(t, db, "subject", time.Now().UTC())
	identical := seedItem(t, db, "identical", time.Now().UTC())
	angled := seedItem(t, db, "angled", time.Now().UTC())
	orthogonal := seedItem(t, db, "orthogonal", time.Now().UTC())
	opposed := seedItem(t, db, "opposed", time.Now().UTC())

	putEmbedding(t, db, subject, testModelNomic, unitVectorFrom(dims, map[int]float32{0: 1}), testHashA)
	putEmbedding(t, db, identical, testModelNomic, unitVectorFrom(dims, map[int]float32{0: 1}), testHashA)
	// 45 degrees from the subject, so similarity ~0.7071.
	putEmbedding(t, db, angled, testModelNomic, unitVectorFrom(dims, map[int]float32{0: 1, 1: 1}), testHashA)
	putEmbedding(t, db, orthogonal, testModelNomic, unitVectorFrom(dims, map[int]float32{1: 1}), testHashA)
	putEmbedding(t, db, opposed, testModelNomic, unitVectorFrom(dims, map[int]float32{0: -1}), testHashA)

	neighbors, err := db.NearestItems(testModelNomic, subject, 10)
	if err != nil {
		t.Fatalf("NearestItems: %v", err)
	}
	if len(neighbors) != 4 {
		t.Fatalf("got %d neighbors, want 4 (every other embedded item)", len(neighbors))
	}

	wantOrder := []int64{identical, angled, orthogonal, opposed}
	for i, want := range wantOrder {
		if neighbors[i].Item.ID != want {
			t.Errorf("neighbor %d is item %d, want %d -- ranking is wrong",
				i, neighbors[i].Item.ID, want)
		}
	}

	// Spot-check the actual values, not just the order: a sign error would
	// still produce a plausible-looking ordering in some arrangements.
	wantSimilarity := []float64{1, math.Sqrt2 / 2, 0, -1}
	for i, want := range wantSimilarity {
		if math.Abs(neighbors[i].Similarity-want) > 1e-6 {
			t.Errorf("neighbor %d similarity = %v, want %v", i, neighbors[i].Similarity, want)
		}
	}
}

func TestNearestItemsExcludesSelf(t *testing.T) {
	db := setupTestDB(t)
	subject := seedItem(t, db, "subject", time.Now().UTC())
	other := seedItem(t, db, "other", time.Now().UTC())

	putEmbedding(t, db, subject, testModelNomic, unitVectorAt(8, 0), testHashA)
	putEmbedding(t, db, other, testModelNomic, unitVectorAt(8, 1), testHashA)

	neighbors, err := db.NearestItems(testModelNomic, subject, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, neighbor := range neighbors {
		if neighbor.Item.ID == subject {
			t.Fatal("the subject appears in its own neighbor list, where it would always rank first")
		}
	}
	if len(neighbors) != 1 {
		t.Errorf("got %d neighbors, want 1", len(neighbors))
	}
}

// Vectors from a different model must not be compared: they are a different
// space, and for nomic vs qwen3 they are not even the same width.
func TestNearestItemsRespectsModel(t *testing.T) {
	db := setupTestDB(t)
	subject := seedItem(t, db, "subject", time.Now().UTC())
	sameModel := seedItem(t, db, "same-model", time.Now().UTC())
	otherModel := seedItem(t, db, "other-model", time.Now().UTC())

	putEmbedding(t, db, subject, testModelNomic, unitVectorAt(768, 0), testHashA)
	putEmbedding(t, db, sameModel, testModelNomic, unitVectorAt(768, 1), testHashA)
	putEmbedding(t, db, otherModel, testModelQwen3, unitVectorAt(1024, 1), testHashA)

	neighbors, err := db.NearestItems(testModelNomic, subject, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(neighbors) != 1 {
		t.Fatalf("got %d neighbors, want 1 (only the same-model item)", len(neighbors))
	}
	if neighbors[0].Item.ID != sameModel {
		t.Errorf("neighbor is item %d, want %d", neighbors[0].Item.ID, sameModel)
	}
}

// limit has to truncate after ranking. Truncating the candidate scan instead
// would return an arbitrary subset that merely looks plausible.
func TestNearestItemsLimitTruncatesAfterRanking(t *testing.T) {
	db := setupTestDB(t)
	const dims = 8
	subject := seedItem(t, db, "subject", time.Now().UTC())
	putEmbedding(t, db, subject, testModelNomic, unitVectorFrom(dims, map[int]float32{0: 1}), testHashA)

	// Seeded worst-first, so a scan that truncated early would keep the wrong
	// ones. The best match is inserted last.
	far := seedItem(t, db, "far", time.Now().UTC())
	mid := seedItem(t, db, "mid", time.Now().UTC())
	near := seedItem(t, db, "near", time.Now().UTC())
	putEmbedding(t, db, far, testModelNomic, unitVectorFrom(dims, map[int]float32{1: 1}), testHashA)
	putEmbedding(t, db, mid, testModelNomic, unitVectorFrom(dims, map[int]float32{0: 1, 1: 1}), testHashA)
	putEmbedding(t, db, near, testModelNomic, unitVectorFrom(dims, map[int]float32{0: 1}), testHashA)

	neighbors, err := db.NearestItems(testModelNomic, subject, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(neighbors) != 1 {
		t.Fatalf("got %d neighbors, want 1", len(neighbors))
	}
	if neighbors[0].Item.ID != near {
		t.Errorf("limit 1 returned item %d, want the best match %d -- the limit must "+
			"apply after ranking, not to the candidate scan", neighbors[0].Item.ID, near)
	}
}

func TestNearestItemsWithoutASubjectEmbedding(t *testing.T) {
	db := setupTestDB(t)
	subject := seedItem(t, db, "subject", time.Now().UTC())

	_, err := db.NearestItems(testModelNomic, subject, 10)
	if err == nil {
		t.Fatal("NearestItems succeeded for an unembedded subject, want an error")
	}
	if !IsNoEmbedding(err) {
		t.Errorf("error %v is not reported as a missing embedding, so the command cannot "+
			"tell the user to run `feedspool embed` rather than reporting a failure", err)
	}
}

func TestNearestItemsWithNoCandidates(t *testing.T) {
	db := setupTestDB(t)
	subject := seedItem(t, db, "subject", time.Now().UTC())
	putEmbedding(t, db, subject, testModelNomic, unitVectorAt(8, 0), testHashA)

	neighbors, err := db.NearestItems(testModelNomic, subject, 10)
	if err != nil {
		t.Fatalf("NearestItems: %v", err)
	}
	if len(neighbors) != 0 {
		t.Errorf("got %d neighbors with nothing else embedded, want 0", len(neighbors))
	}
}

// A row whose width disagrees with the subject's can only be corruption --
// the model filter already guarantees one space. Skip it rather than failing
// the whole query, so one bad row cannot break `related` for everything.
func TestNearestItemsSkipsMismatchedWidths(t *testing.T) {
	db := setupTestDB(t)
	subject := seedItem(t, db, "subject", time.Now().UTC())
	good := seedItem(t, db, "good", time.Now().UTC())
	corrupt := seedItem(t, db, "corrupt", time.Now().UTC())

	putEmbedding(t, db, subject, testModelNomic, unitVectorAt(8, 0), testHashA)
	putEmbedding(t, db, good, testModelNomic, unitVectorAt(8, 1), testHashA)
	// Same model, different width: only reachable by corruption.
	putEmbedding(t, db, corrupt, testModelNomic, unitVectorAt(16, 1), testHashA)

	neighbors, err := db.NearestItems(testModelNomic, subject, 10)
	if err != nil {
		t.Fatalf("NearestItems should skip the bad row, not fail: %v", err)
	}
	if len(neighbors) != 1 {
		t.Fatalf("got %d neighbors, want 1", len(neighbors))
	}
	if neighbors[0].Item.ID != good {
		t.Errorf("neighbor is item %d, want %d", neighbors[0].Item.ID, good)
	}
}

func TestNearestItemsRejectsNonPositiveLimit(t *testing.T) {
	db := setupTestDB(t)
	subject := seedItem(t, db, "subject", time.Now().UTC())
	putEmbedding(t, db, subject, testModelNomic, unitVectorAt(8, 0), testHashA)

	for _, limit := range []int{0, -1} {
		if _, err := db.NearestItems(testModelNomic, subject, limit); err == nil {
			t.Errorf("NearestItems with limit %d succeeded, want an error", limit)
		}
	}
}

// The neighbor's Item has to be fully populated, since the command prints its
// title and link.
func TestNearestItemsReturnsPopulatedItems(t *testing.T) {
	db := setupTestDB(t)
	subject := seedItem(t, db, "subject", time.Now().UTC())
	other := seedItem(t, db, "other", time.Now().UTC())
	putEmbedding(t, db, subject, testModelNomic, unitVectorAt(8, 0), testHashA)
	putEmbedding(t, db, other, testModelNomic, unitVectorAt(8, 1), testHashA)

	neighbors, err := db.NearestItems(testModelNomic, subject, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(neighbors) != 1 {
		t.Fatalf("got %d neighbors, want 1", len(neighbors))
	}
	item := neighbors[0].Item
	if item.Title != "Item other" {
		t.Errorf("Title = %q, want %q", item.Title, "Item other")
	}
	if item.Link == "" || item.FeedURL == "" || item.GUID == "" {
		t.Errorf("neighbor item is not fully populated: %+v", item)
	}
}
