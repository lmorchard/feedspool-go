package database

import (
	"database/sql"
	"fmt"

	"github.com/sirupsen/logrus"
)

// DerivedBackfill describes an artifact derived from items that must be
// recomputed when the item changes or the generator's version moves. #58
// implements it for FTS text; #30 implements it for embeddings.
type DerivedBackfill interface {
	Name() string
	Version() int
	// NextBatch returns up to limit item IDs still needing work, all with
	// id > afterID, in ascending id order.
	NextBatch(tx *sql.Tx, afterID int64, limit int) ([]int64, error)
	// Recompute derives and stores the artifact for those item IDs.
	Recompute(tx *sql.Tx, ids []int64) error
	// Remaining reports how many items still need work, for progress output.
	Remaining(tx *sql.Tx) (int64, error)
}

const defaultBackfillBatchSize = 500

// RunBackfill processes the generator in committed batches, so an interrupted
// run resumes where it stopped instead of restarting, and a large database is
// never held in one transaction.
//
// progress may be nil. It is called after each committed batch with the number
// of items this run has processed and the number it found outstanding when it
// started; a resumed run therefore counts from zero against the smaller
// remainder, not against the whole corpus.
func (db *DB) RunBackfill(gen DerivedBackfill, batchSize int, progress func(done, total int64)) error {
	if batchSize <= 0 {
		batchSize = defaultBackfillBatchSize
	}

	total, err := db.backfillRemaining(gen)
	if err != nil {
		return err
	}
	// Name and version together identify a run. After a version bump this line
	// is what says whether the reindex it was supposed to force actually found
	// anything to do.
	logrus.Debugf("Starting %s backfill v%d: %d items outstanding, batch size %d",
		gen.Name(), gen.Version(), total, batchSize)

	var done, afterID int64
	for {
		ids, err := db.runBackfillBatch(gen, afterID, batchSize)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		// Advancing the cursor past the largest ID in the batch is what makes
		// the loop terminate: a row the generator cannot bring up to date is
		// skipped on the next pass instead of being selected forever.
		afterID = ids[len(ids)-1]
		done += int64(len(ids))
		if progress != nil {
			progress(done, total)
		}
	}
}

// backfillRemaining reads the outstanding count in its own short transaction.
func (db *DB) backfillRemaining(gen DerivedBackfill) (int64, error) {
	tx, err := db.conn.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin %s backfill count: %w", gen.Name(), err)
	}
	// Read-only, so it is always rolled back rather than committed.
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			logrus.WithError(rollbackErr).Warn("Failed to roll back backfill count transaction")
		}
	}()

	remaining, err := gen.Remaining(tx)
	if err != nil {
		return 0, fmt.Errorf("failed to count remaining %s work: %w", gen.Name(), err)
	}
	return remaining, nil
}

// runBackfillBatch recomputes one batch and commits it, returning the IDs it
// processed. An empty result means there is nothing left to do.
func (db *DB) runBackfillBatch(gen DerivedBackfill, afterID int64, batchSize int) ([]int64, error) {
	tx, err := db.conn.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin %s backfill batch: %w", gen.Name(), err)
	}
	committed := false
	defer func() {
		if !committed {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				logrus.WithError(rollbackErr).Warn("Failed to roll back backfill batch")
			}
		}
	}()

	ids, err := gen.NextBatch(tx, afterID, batchSize)
	if err != nil {
		return nil, fmt.Errorf("failed to select %s backfill batch: %w", gen.Name(), err)
	}
	if len(ids) == 0 {
		return nil, nil
	}

	if err := gen.Recompute(tx, ids); err != nil {
		return nil, fmt.Errorf("failed to recompute %s v%d for %d items: %w",
			gen.Name(), gen.Version(), len(ids), err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit %s backfill batch: %w", gen.Name(), err)
	}
	committed = true
	return ids, nil
}
