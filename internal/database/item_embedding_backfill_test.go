package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// fakeProvider is an embed.Provider that returns deterministic one-hot unit
// vectors, so the generator can be exercised with no Ollama in the picture.
type fakeProvider struct {
	modelID string
	dims    int
	calls   int
	texts   []string
	err     error
}

func newFakeProvider(modelID string, dims int) *fakeProvider {
	return &fakeProvider{modelID: modelID, dims: dims}
}

func (p *fakeProvider) ModelID() string { return p.modelID }

func (p *fakeProvider) Embed(_ context.Context, texts []string) ([][]float32, error) {
	p.calls++
	p.texts = append(p.texts, texts...)
	if p.err != nil {
		return nil, p.err
	}
	vectors := make([][]float32, len(texts))
	for i := range texts {
		v := make([]float32, p.dims)
		v[i%p.dims] = 1
		vectors[i] = v
	}
	return vectors, nil
}

// embedWindow is a window wide enough to hold everything the helpers seed.
func embedWindow() (since, until time.Time) {
	now := time.Now().UTC()
	return now.Add(-24 * time.Hour), now.Add(24 * time.Hour)
}

func countEmbeddingRows(t *testing.T, db *DB, modelID string) int {
	t.Helper()
	var n int
	if err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM item_embeddings WHERE model_id = ?`, modelID,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestEmbedItemsFillsTheWindow(t *testing.T) {
	db := setupTestDB(t)
	for i := range 5 {
		seedItem(t, db, fmt.Sprintf("e-%03d", i), time.Now().UTC())
	}

	provider := newFakeProvider(testModelNomic, 768)
	since, until := embedWindow()
	if err := db.EmbedItems(context.Background(), provider, since, until, false, 2, nil); err != nil {
		t.Fatalf("EmbedItems: %v", err)
	}

	if got := countEmbeddingRows(t, db, testModelNomic); got != 5 {
		t.Errorf("embedded %d items, want 5", got)
	}
	// Batch size 2 over 5 items.
	if provider.calls != 3 {
		t.Errorf("provider called %d times, want 3", provider.calls)
	}
}

// All four staleness cases, plus the negative that matters most: an
// up-to-date row must select no work, or every run re-embeds the whole window.
func TestEmbedStaleness(t *testing.T) {
	since, until := embedWindow()

	t.Run("no row for this model is work", func(t *testing.T) {
		db := setupTestDB(t)
		seedItem(t, db, "e-1", time.Now().UTC())

		n, err := db.CountItemsToEmbed(testModelNomic, since, until, false)
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("outstanding = %d, want 1", n)
		}
	})

	t.Run("an up-to-date row is not work", func(t *testing.T) {
		db := setupTestDB(t)
		seedItem(t, db, "e-1", time.Now().UTC())

		provider := newFakeProvider(testModelNomic, 768)
		if err := db.EmbedItems(context.Background(), provider, since, until, false, 8, nil); err != nil {
			t.Fatal(err)
		}
		n, err := db.CountItemsToEmbed(testModelNomic, since, until, false)
		if err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("outstanding = %d after embedding, want 0 -- otherwise every run "+
				"re-embeds the whole window", n)
		}
	})

	t.Run("changed item text is work", func(t *testing.T) {
		db := setupTestDB(t)
		id := seedItem(t, db, "e-1", time.Now().UTC())

		provider := newFakeProvider(testModelNomic, 768)
		if err := db.EmbedItems(context.Background(), provider, since, until, false, 8, nil); err != nil {
			t.Fatal(err)
		}

		// Simulate the item being revised: item_text's hash moves on.
		if _, err := db.conn.Exec(
			`UPDATE item_text SET source_hash = 'moved-on' WHERE item_id = ?`, id,
		); err != nil {
			t.Fatal(err)
		}

		n, err := db.CountItemsToEmbed(testModelNomic, since, until, false)
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("outstanding = %d after the item's text changed, want 1. Embeddings "+
				"have no trigger and no live write path, so the hash comparison is the "+
				"only thing that notices", n)
		}
	})

	t.Run("an older generator version is work", func(t *testing.T) {
		db := setupTestDB(t)
		id := seedItem(t, db, "e-1", time.Now().UTC())

		provider := newFakeProvider(testModelNomic, 768)
		if err := db.EmbedItems(context.Background(), provider, since, until, false, 8, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := db.conn.Exec(
			`UPDATE item_embeddings SET generator_version = generator_version - 1 WHERE item_id = ?`, id,
		); err != nil {
			t.Fatal(err)
		}

		n, err := db.CountItemsToEmbed(testModelNomic, since, until, false)
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("outstanding = %d after a version rollback, want 1", n)
		}
	})
}

// Embedding one model must leave the other's rows alone and still report the
// other's items as outstanding. This is what makes a bake-off possible.
func TestEmbedModelsAreIndependent(t *testing.T) {
	db := setupTestDB(t)
	for i := range 3 {
		seedItem(t, db, fmt.Sprintf("e-%03d", i), time.Now().UTC())
	}
	since, until := embedWindow()

	if err := db.EmbedItems(context.Background(),
		newFakeProvider(testModelNomic, 768), since, until, false, 8, nil); err != nil {
		t.Fatal(err)
	}

	outstanding, err := db.CountItemsToEmbed(testModelQwen3, since, until, false)
	if err != nil {
		t.Fatal(err)
	}
	if outstanding != 3 {
		t.Errorf("qwen3 outstanding = %d after embedding nomic, want 3", outstanding)
	}

	if err := db.EmbedItems(context.Background(),
		newFakeProvider(testModelQwen3, 1024), since, until, false, 8, nil); err != nil {
		t.Fatal(err)
	}

	if got := countEmbeddingRows(t, db, testModelNomic); got != 3 {
		t.Errorf("nomic rows = %d after embedding qwen3, want 3 (untouched)", got)
	}
	if got := countEmbeddingRows(t, db, testModelQwen3); got != 3 {
		t.Errorf("qwen3 rows = %d, want 3", got)
	}
}

func TestEmbedForceReselectsUpToDateRows(t *testing.T) {
	db := setupTestDB(t)
	seedItem(t, db, "e-1", time.Now().UTC())
	since, until := embedWindow()

	provider := newFakeProvider(testModelNomic, 768)
	if err := db.EmbedItems(context.Background(), provider, since, until, false, 8, nil); err != nil {
		t.Fatal(err)
	}

	normal, err := db.CountItemsToEmbed(testModelNomic, since, until, false)
	if err != nil {
		t.Fatal(err)
	}
	forced, err := db.CountItemsToEmbed(testModelNomic, since, until, true)
	if err != nil {
		t.Fatal(err)
	}
	if normal != 0 {
		t.Errorf("normal outstanding = %d, want 0", normal)
	}
	if forced != 1 {
		t.Errorf("forced outstanding = %d, want 1 -- force must re-select rows the "+
			"staleness predicate deliberately skips", forced)
	}
}

func TestEmbedWindowBounds(t *testing.T) {
	db := setupTestDB(t)

	base := time.Now().UTC().Truncate(time.Second)
	inside := seedItem(t, db, "inside", base.Add(-2*time.Hour))
	seedItem(t, db, "too-old", base.Add(-48*time.Hour))
	seedItem(t, db, "too-new", base.Add(48*time.Hour))

	since := base.Add(-3 * time.Hour)
	until := base.Add(1 * time.Hour)

	provider := newFakeProvider(testModelNomic, 768)
	if err := db.EmbedItems(context.Background(), provider, since, until, false, 8, nil); err != nil {
		t.Fatal(err)
	}

	if got := countEmbeddingRows(t, db, testModelNomic); got != 1 {
		t.Fatalf("embedded %d items, want only the one inside the window", got)
	}
	if _, err := db.GetItemEmbedding(inside, testModelNomic); err != nil {
		t.Errorf("the in-window item was not the one embedded: %v", err)
	}
}

// "too-new" above is clamped by the fetcher on the real write path, but the
// window arithmetic still has to be right at the edges.
func TestEmbedWindowIsInclusiveAtBothEnds(t *testing.T) {
	db := setupTestDB(t)

	base := time.Now().UTC().Truncate(time.Second)
	seedItem(t, db, "at-since", base.Add(-time.Hour))
	seedItem(t, db, "at-until", base)

	n, err := db.CountItemsToEmbed(testModelNomic, base.Add(-time.Hour), base, false)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("outstanding = %d for a window closed exactly on both items, want 2", n)
	}
}

// published_date is nullable, and an item that has none must still be
// selectable on its first_seen -- that is the whole point of COALESCE, and it
// is why windowing on published_date alone would have been wrong.
func TestEmbedFallsBackToFirstSeenWhenPublishedIsNull(t *testing.T) {
	db := setupTestDB(t)
	id := seedItem(t, db, "no-published", time.Now().UTC())

	// Drop the published date but keep first_seen, which is the real shape of
	// an item from a feed that publishes no dates.
	if _, err := db.conn.Exec(
		`UPDATE items SET published_date = NULL WHERE id = ?`, id,
	); err != nil {
		t.Fatal(err)
	}
	var firstSeen sql.NullString
	if err := db.conn.QueryRow(
		`SELECT first_seen FROM items WHERE id = ?`, id,
	).Scan(&firstSeen); err != nil {
		t.Fatal(err)
	}
	if !firstSeen.Valid {
		t.Fatal("fixture has no first_seen, so this would test COALESCE falling back to NULL")
	}

	since, until := embedWindow()
	n, err := db.CountItemsToEmbed(testModelNomic, since, until, false)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("outstanding = %d for an item with no published_date, want 1 "+
			"(it should be selected on first_seen)", n)
	}
}

// The inner JOIN on item_text skips items with no derived text rather than
// embedding empty strings. Those have to be counted, or a user silently gets
// fewer embeddings than items and no explanation.
func TestEmbedSkipsAndCountsItemsWithoutText(t *testing.T) {
	db := setupTestDB(t)
	withText := seedItem(t, db, "has-text", time.Now().UTC())
	without := seedItem(t, db, "no-text", time.Now().UTC())

	if _, err := db.conn.Exec(`DELETE FROM item_text WHERE item_id = ?`, without); err != nil {
		t.Fatal(err)
	}

	since, until := embedWindow()
	provider := newFakeProvider(testModelNomic, 768)
	if err := db.EmbedItems(context.Background(), provider, since, until, false, 8, nil); err != nil {
		t.Fatal(err)
	}

	if got := countEmbeddingRows(t, db, testModelNomic); got != 1 {
		t.Errorf("embedded %d items, want 1 (the one with derived text)", got)
	}
	if _, err := db.GetItemEmbedding(withText, testModelNomic); err != nil {
		t.Errorf("the item with text was not embedded: %v", err)
	}

	missing, err := db.CountItemsMissingText(since, until)
	if err != nil {
		t.Fatal(err)
	}
	if missing != 1 {
		t.Errorf("CountItemsMissingText = %d, want 1 -- the command needs this to point "+
			"the user at `feedspool reindex`", missing)
	}
}

// The embed input is assembled from item_text, not re-derived from items. This
// checks the provider actually received the derived text.
func TestEmbedSendsDerivedText(t *testing.T) {
	db := setupTestDB(t)
	seedItem(t, db, "e-1", time.Now().UTC())

	since, until := embedWindow()
	provider := newFakeProvider(testModelNomic, 768)
	if err := db.EmbedItems(context.Background(), provider, since, until, false, 8, nil); err != nil {
		t.Fatal(err)
	}

	if len(provider.texts) != 1 {
		t.Fatalf("provider saw %d texts, want 1", len(provider.texts))
	}
	text := provider.texts[0]
	for _, want := range []string{"Item e-1", "summary for e-1", "content for e-1"} {
		if !strings.Contains(text, want) {
			t.Errorf("embed input %q does not contain %q; title, summary and body must all reach the model", text, want)
		}
	}
}

func TestEmbedPropagatesProviderErrors(t *testing.T) {
	db := setupTestDB(t)
	seedItem(t, db, "e-1", time.Now().UTC())

	sentinel := errors.New("provider exploded")
	provider := newFakeProvider(testModelNomic, 768)
	provider.err = sentinel

	since, until := embedWindow()
	err := db.EmbedItems(context.Background(), provider, since, until, false, 8, nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("EmbedItems error = %v, want the provider's error wrapped", err)
	}
	if got := countEmbeddingRows(t, db, testModelNomic); got != 0 {
		t.Errorf("%d rows written despite the provider failing, want 0", got)
	}
}

// The window expression must be the one migration 9 indexed, textually. An
// equivalent hand-written COALESCE compiles, returns correct rows, and
// silently full-scans -- which is invisible until the corpus is large.
func TestEmbedQueryUsesTheEffectiveDateIndex(t *testing.T) {
	db := setupTestDB(t)
	seedItem(t, db, "e-1", time.Now().UTC())

	gen := &itemEmbeddingBackfill{modelID: testModelNomic}
	gen.since, gen.until = embedWindow()

	query, args := gen.batchQuery(0, 8)
	rows, err := db.conn.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}
	defer rows.Close()

	var plan strings.Builder
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(detail)
		plan.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(plan.String(), "idx_items_effective_date") {
		t.Errorf("the embed batch query does not use idx_items_effective_date, so it is "+
			"full-scanning items. The window expression must match migration 9's indexed "+
			"expression textually.\nplan:\n%s", plan.String())
	}
}

// The staleness predicate names item_text's hash, so the two definitions have
// to stay in step. This pins that the predicate reads from item_text rather
// than from a copy of the hash.
func TestEmbedStalenessComparesAgainstItemText(t *testing.T) {
	if !strings.Contains(itemEmbeddingStaleness, "t.source_hash") {
		t.Error("the staleness predicate does not compare against item_text.source_hash; " +
			"a revised item would keep a stale vector forever")
	}
}
