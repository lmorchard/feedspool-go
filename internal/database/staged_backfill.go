package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/sirupsen/logrus"
)

// StagedInput is one item's input to a staged generator, read inside a short
// transaction so the slow phase has nothing open.
type StagedInput struct {
	ItemID int64
	// Text is the input handed to the external worker.
	Text string
	// SourceHash is carried through unchanged so Write can record what the
	// vector was derived from without re-reading it.
	SourceHash string
}

// StagedResult is what Compute produced for one item.
type StagedResult struct {
	ItemID     int64
	SourceHash string
	Vector     []float32
}

// StagedBackfill is DerivedBackfill for generators whose work is slow and
// external, and it exists for one reason: db.go sets SetMaxOpenConns(1), so an
// open *sql.Tx has checked out the only connection and blocks every other
// database user in the process until it commits.
//
// RunBackfill calls Recompute inside its transaction, which is right for
// item_text (pure CPU, microseconds) and wrong for anything that makes a
// network call. At ~80 items/s and a 500-item batch that would be seconds of
// total database unavailability per batch, against a 5s busy_timeout -- a
// `serve` sharing the process would start failing requests.
//
// Compute therefore takes a context.Context and no *sql.Tx. The type signature
// is the enforcement; TestRunStagedBackfillHoldsNoTransactionDuringCompute is
// the proof the driver honors it.
type StagedBackfill interface {
	Name() string
	Version() int
	// NextBatch returns up to limit item IDs still needing work, all with
	// id > afterID, in ascending id order.
	NextBatch(tx *sql.Tx, afterID int64, limit int) ([]int64, error)
	// ReadInputs reads what Compute needs, inside the same short transaction.
	ReadInputs(tx *sql.Tx, ids []int64) ([]StagedInput, error)
	// Compute does the slow external work. It gets no transaction, on purpose.
	Compute(ctx context.Context, inputs []StagedInput) ([]StagedResult, error)
	// Write stores the results in a second short transaction.
	Write(tx *sql.Tx, results []StagedResult) error
	// Remaining reports how many items still need work, for progress output.
	Remaining(tx *sql.Tx) (int64, error)
}

const defaultStagedBatchSize = 64

// RunStagedBackfill processes the generator in committed batches, splitting
// each into a short read transaction, the slow work with no transaction open,
// and a short write transaction.
//
// An interrupted run resumes where it stopped rather than restarting, because
// every batch is committed as it completes.
//
// progress may be nil. It is called after each committed batch with the number
// of items this run has processed and the number it found outstanding when it
// started -- so a resumed run counts from zero against the smaller remainder,
// matching RunBackfill.
func (db *DB) RunStagedBackfill(
	ctx context.Context, gen StagedBackfill, batchSize int, progress func(done, total int64),
) error {
	if batchSize <= 0 {
		batchSize = defaultStagedBatchSize
	}

	total, err := db.stagedRemaining(gen)
	if err != nil {
		return err
	}
	logrus.Debugf("Starting %s staged backfill v%d: %d items outstanding, batch size %d",
		gen.Name(), gen.Version(), total, batchSize)

	var done, afterID int64
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%s backfill stopped: %w", gen.Name(), err)
		}

		inputs, lastID, selected, err := db.stagedReadBatch(gen, afterID, batchSize)
		if err != nil {
			return err
		}
		// Only an empty *selection* means the work is done. A batch that
		// selected IDs but yielded no inputs -- a generator that legitimately
		// dropped every row in it -- must still advance the cursor, or every
		// later ID goes unprocessed while the run reports success.
		if selected == 0 {
			return nil
		}
		if len(inputs) == 0 {
			afterID = lastID
			continue
		}

		// No transaction is open here. That is the entire point of this driver.
		results, err := gen.Compute(ctx, inputs)
		if err != nil {
			return fmt.Errorf("failed to compute %s v%d for %d items: %w",
				gen.Name(), gen.Version(), len(inputs), err)
		}

		if err := db.stagedWriteBatch(gen, results); err != nil {
			return err
		}

		// Advancing past the largest ID in the batch is what makes the loop
		// terminate: a row the generator cannot bring up to date is skipped on
		// the next pass instead of being selected forever. Same argument as
		// RunBackfill.
		afterID = lastID
		done += int64(len(inputs))
		if progress != nil {
			progress(done, total)
		}
	}
}

// stagedRemaining reads the outstanding count in its own short transaction.
func (db *DB) stagedRemaining(gen StagedBackfill) (int64, error) {
	tx, err := db.conn.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin %s backfill count: %w", gen.Name(), err)
	}
	// Read-only, so always rolled back rather than committed.
	defer rollbackUnlessDone(tx, "staged backfill count transaction")

	remaining, err := gen.Remaining(tx)
	if err != nil {
		return 0, fmt.Errorf("failed to count remaining %s work: %w", gen.Name(), err)
	}
	return remaining, nil
}

// stagedReadBatch selects a batch and reads its inputs, then releases the
// connection before returning.
//
// selected is the number of IDs NextBatch chose, reported separately from
// len(inputs) because the caller has to tell "nothing left to do" (selected
// zero) from "this batch yielded no work but later IDs remain" (selected
// non-zero, inputs empty). Collapsing the two ends the run early.
func (db *DB) stagedReadBatch(
	gen StagedBackfill, afterID int64, batchSize int,
) (inputs []StagedInput, lastID int64, selected int, err error) {
	tx, err := db.conn.Begin()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to begin %s backfill read: %w", gen.Name(), err)
	}
	// Read-only, so always rolled back. Rolling back rather than committing is
	// also what releases the connection before Compute runs.
	defer rollbackUnlessDone(tx, "staged backfill read transaction")

	ids, err := gen.NextBatch(tx, afterID, batchSize)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to select %s backfill batch: %w", gen.Name(), err)
	}
	if len(ids) == 0 {
		return nil, 0, 0, nil
	}

	inputs, err = gen.ReadInputs(tx, ids)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to read %s backfill inputs: %w", gen.Name(), err)
	}

	// The cursor advances on the IDs selected, not the inputs returned: a
	// generator that legitimately drops an item still has to make progress.
	return inputs, ids[len(ids)-1], len(ids), nil
}

// stagedWriteBatch commits one batch of results.
func (db *DB) stagedWriteBatch(gen StagedBackfill, results []StagedResult) error {
	if len(results) == 0 {
		return nil
	}

	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin %s backfill write: %w", gen.Name(), err)
	}
	defer rollbackUnlessDone(tx, "staged backfill write transaction")

	if err := gen.Write(tx, results); err != nil {
		return fmt.Errorf("failed to store %s v%d for %d items: %w",
			gen.Name(), gen.Version(), len(results), err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit %s backfill batch: %w", gen.Name(), err)
	}
	return nil
}
