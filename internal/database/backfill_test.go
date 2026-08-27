package database

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/lmorchard/feedspool-go/internal/itemtext"
)

// errBackfillInterrupted stands in for a crash, a SIGINT, or a disk error
// partway through a long backfill.
var errBackfillInterrupted = errors.New("backfill interrupted")

// interruptedBackfill runs the real generator for allowedBatches batches and
// then fails, leaving whatever was already committed on disk.
type interruptedBackfill struct {
	DerivedBackfill
	allowedBatches int
	batches        int
}

func (g *interruptedBackfill) NextBatch(tx *sql.Tx, afterID int64, limit int) ([]int64, error) {
	if g.batches >= g.allowedBatches {
		return nil, errBackfillInterrupted
	}
	g.batches++
	return g.DerivedBackfill.NextBatch(tx, afterID, limit)
}

func TestRunBackfillIsResumable(t *testing.T) {
	const (
		itemCount = 25
		batchSize = 10
	)
	db := seedSearchableItems(t, itemCount)
	// Phase 3's UpsertItem derives item_text on write, so the seeded items are
	// already indexed. Discard that to reproduce the case this test is about:
	// existing items whose derived text is missing or stale, which is what
	// gives the interrupted backfill real work to interrupt.
	execSQL(t, db, `DELETE FROM item_text`)

	var reported [][2]int64
	interrupted := &interruptedBackfill{
		DerivedBackfill: newItemTextBackfill(itemtext.DefaultOptions()),
		allowedBatches:  1,
	}
	err := db.RunBackfill(interrupted, batchSize, func(done, total int64) {
		reported = append(reported, [2]int64{done, total})
	})
	if !errors.Is(err, errBackfillInterrupted) {
		t.Fatalf("RunBackfill() error = %v, want %v", err, errBackfillInterrupted)
	}
	if got := countItemTextRows(t, db); got != batchSize {
		t.Errorf("after the interrupted run item_text has %d rows, want the one committed batch of %d",
			got, batchSize)
	}
	if len(reported) != 1 || reported[0] != [2]int64{batchSize, itemCount} {
		t.Errorf("progress reported %v, want one call of (%d, %d)", reported, batchSize, itemCount)
	}
	integrityCheck(t, db)

	// Resuming picks up where the interrupted run stopped rather than starting over.
	reported = reported[:0]
	if err := db.RunBackfill(
		newItemTextBackfill(itemtext.DefaultOptions()), batchSize,
		func(done, total int64) { reported = append(reported, [2]int64{done, total}) },
	); err != nil {
		t.Fatalf("resumed RunBackfill() error = %v", err)
	}
	if len(reported) == 0 || reported[0][1] != itemCount-batchSize {
		t.Errorf("resumed run reported %v, want a remaining total of %d", reported, itemCount-batchSize)
	}
	if got := countItemTextRows(t, db); got != itemCount {
		t.Errorf("item_text has %d rows, want %d", got, itemCount)
	}
	if got := countIndexedItems(t, db); got != itemCount {
		t.Errorf("index holds %d items, want %d", got, itemCount)
	}
	integrityCheck(t, db)
}

// TestRunBackfillTerminatesOnUnfixableRow guards the ascending-ID cursor.
// Without it, a row the generator cannot bring up to date would be selected
// forever and the loop would never end.
func TestRunBackfillTerminatesOnUnfixableRow(t *testing.T) {
	const itemCount = 3
	db := seedSearchableItems(t, itemCount)

	stuck := &noOpBackfill{db: db}
	if err := db.RunBackfill(stuck, 1, nil); err != nil {
		t.Fatalf("RunBackfill() error = %v", err)
	}
	// One batch per item, plus the empty batch that ends the loop.
	if want := itemCount + 1; stuck.batches != want {
		t.Errorf("generator saw %d batches, want %d -- the cursor did not advance", stuck.batches, want)
	}
}

// noOpBackfill reports every item as needing work and never writes anything.
type noOpBackfill struct {
	db      *DB
	batches int
}

func (g *noOpBackfill) Name() string { return "noop" }
func (g *noOpBackfill) Version() int { return 1 }

func (g *noOpBackfill) NextBatch(tx *sql.Tx, afterID int64, limit int) ([]int64, error) {
	g.batches++
	rows, err := tx.Query(`SELECT id FROM items WHERE id > ? ORDER BY id LIMIT ?`, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (g *noOpBackfill) Recompute(_ *sql.Tx, _ []int64) error { return nil }

func (g *noOpBackfill) Remaining(tx *sql.Tx) (int64, error) {
	var remaining int64
	err := tx.QueryRow(`SELECT COUNT(*) FROM items`).Scan(&remaining)
	return remaining, err
}
