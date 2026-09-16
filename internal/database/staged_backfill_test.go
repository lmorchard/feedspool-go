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

// The deadline the no-transaction probe waits on. Long enough that a free
// connection is obtained comfortably, short enough that a held one fails fast.
const stagedProbeTimeout = 250 * time.Millisecond

// errStagedComputeFailed is the injected failure for the resume test.
var errStagedComputeFailed = errors.New("injected compute failure")

// fakeStagedGen is a StagedBackfill over a scratch table, so the driver can be
// exercised with no embedding provider in the picture. It derives an uppercase
// title, which is the cheapest stand-in for "slow external work".
type fakeStagedGen struct {
	db *DB

	// computeHook runs inside Compute, which is where the driver must not be
	// holding a transaction.
	computeHook func() error
	// failAfter makes Compute fail once it has produced this many items, for
	// the resume test. Zero means never.
	failAfter int
	// skipWrite leaves rows selectable after a batch, so the cursor is the only
	// thing that can terminate the loop.
	skipWrite bool

	computed int
	batches  int
}

func newStagedProbeTable(t *testing.T, db *DB) {
	t.Helper()
	if _, err := db.conn.Exec(
		`CREATE TABLE staged_probe (
			item_id INTEGER PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
			value   TEXT NOT NULL
		)`,
	); err != nil {
		t.Fatal(err)
	}
}

func (g *fakeStagedGen) Name() string { return "stagedprobe" }
func (g *fakeStagedGen) Version() int { return 1 }

func (g *fakeStagedGen) workCondition() string {
	if g.skipWrite {
		return "TRUE"
	}
	return "p.item_id IS NULL"
}

func (g *fakeStagedGen) NextBatch(tx *sql.Tx, afterID int64, limit int) ([]int64, error) {
	rows, err := tx.Query(
		`SELECT i.id FROM items i
		 LEFT JOIN staged_probe p ON p.item_id = i.id
		 WHERE i.id > ? AND (`+g.workCondition()+`)
		 ORDER BY i.id LIMIT ?`,
		afterID, limit,
	)
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

func (g *fakeStagedGen) ReadInputs(tx *sql.Tx, ids []int64) ([]StagedInput, error) {
	inputs := make([]StagedInput, 0, len(ids))
	for _, id := range ids {
		var title string
		if err := tx.QueryRow(`SELECT title FROM items WHERE id = ?`, id).Scan(&title); err != nil {
			return nil, err
		}
		inputs = append(inputs, StagedInput{ItemID: id, Text: title, SourceHash: "hash-" + title})
	}
	return inputs, nil
}

func (g *fakeStagedGen) Compute(_ context.Context, inputs []StagedInput) ([]StagedResult, error) {
	g.batches++
	if g.computeHook != nil {
		if err := g.computeHook(); err != nil {
			return nil, err
		}
	}

	results := make([]StagedResult, 0, len(inputs))
	for _, in := range inputs {
		if g.failAfter > 0 && g.computed >= g.failAfter {
			return nil, errStagedComputeFailed
		}
		g.computed++
		results = append(results, StagedResult{
			ItemID:     in.ItemID,
			SourceHash: in.SourceHash,
			Vector:     []float32{1, 0, 0},
		})
	}
	return results, nil
}

func (g *fakeStagedGen) Write(tx *sql.Tx, results []StagedResult) error {
	if g.skipWrite {
		return nil
	}
	for _, r := range results {
		if _, err := tx.Exec(
			`INSERT OR REPLACE INTO staged_probe (item_id, value) VALUES (?, ?)`,
			r.ItemID, strings.ToUpper(r.SourceHash),
		); err != nil {
			return err
		}
	}
	return nil
}

func (g *fakeStagedGen) Remaining(tx *sql.Tx) (int64, error) {
	var remaining int64
	err := tx.QueryRow(
		`SELECT COUNT(*) FROM items i
		 LEFT JOIN staged_probe p ON p.item_id = i.id
		 WHERE ` + g.workCondition(),
	).Scan(&remaining)
	return remaining, err
}

func seedStagedItems(t *testing.T, db *DB, count int) {
	t.Helper()
	for i := range count {
		seedItem(t, db, fmt.Sprintf("staged-%03d", i), time.Now().UTC())
	}
}

func countStagedProbeRows(t *testing.T, db *DB) int {
	t.Helper()
	var n int
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM staged_probe`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// This is the test the whole driver exists for.
//
// db.go sets SetMaxOpenConns(1), so an open *sql.Tx has checked out the only
// connection and nothing else in the process can reach the database until it
// commits. RunBackfill calls Recompute inside its transaction, which is
// correct for pure-CPU work and wrong for a network call: at ~80 items/s and a
// 500-item batch that would be seconds of total unavailability against a 5s
// busy_timeout, and a `serve` sharing the process would start failing.
//
// Compute therefore receives no *sql.Tx. This asserts the driver actually
// honors that, by having Compute obtain a connection under a short deadline --
// which is impossible if a transaction is still open.
func TestRunStagedBackfillHoldsNoTransactionDuringCompute(t *testing.T) {
	db := setupTestDB(t)
	newStagedProbeTable(t, db)
	seedStagedItems(t, db, 6)

	probes := 0
	gen := &fakeStagedGen{
		db: db,
		computeHook: func() error {
			probes++
			ctx, cancel := context.WithTimeout(context.Background(), stagedProbeTimeout)
			defer cancel()

			var one int
			if err := db.conn.QueryRowContext(ctx, `SELECT 1`).Scan(&one); err != nil {
				return fmt.Errorf(
					"could not reach the database during Compute, so a transaction was "+
						"open across it -- that is the whole failure this driver prevents: %w", err,
				)
			}
			return nil
		},
	}

	if err := db.RunStagedBackfill(context.Background(), gen, 2, nil); err != nil {
		t.Fatalf("RunStagedBackfill: %v", err)
	}
	if probes < 2 {
		t.Fatalf("Compute ran %d times; too few to prove anything about batch boundaries", probes)
	}
	if got := countStagedProbeRows(t, db); got != 6 {
		t.Errorf("wrote %d rows, want 6", got)
	}
}

// An interrupted run must leave earlier batches committed, so restarting
// resumes rather than starting over. That is what makes a slow network
// backfill over a large spool survivable.
func TestRunStagedBackfillResumesAfterFailure(t *testing.T) {
	db := setupTestDB(t)
	newStagedProbeTable(t, db)
	seedStagedItems(t, db, 10)

	// Fail partway through the third batch of 2.
	failing := &fakeStagedGen{db: db, failAfter: 5}
	err := db.RunStagedBackfill(context.Background(), failing, 2, nil)
	if !errors.Is(err, errStagedComputeFailed) {
		t.Fatalf("RunStagedBackfill error = %v, want the injected failure", err)
	}

	committed := countStagedProbeRows(t, db)
	if committed == 0 {
		t.Fatal("no rows survived the failure; batches are not being committed as they go")
	}
	if committed == 10 {
		t.Fatal("all rows were written despite the injected failure")
	}

	// A fresh run finishes the remainder.
	resuming := &fakeStagedGen{db: db}
	if err := db.RunStagedBackfill(context.Background(), resuming, 2, nil); err != nil {
		t.Fatalf("resumed RunStagedBackfill: %v", err)
	}
	if got := countStagedProbeRows(t, db); got != 10 {
		t.Errorf("after resuming, %d rows written, want 10", got)
	}
	if resuming.computed != 10-committed {
		t.Errorf("resumed run computed %d items, want %d -- it should process only the "+
			"remainder, not the whole corpus", resuming.computed, 10-committed)
	}
}

// A generator whose Write is a no-op leaves every row selectable forever. The
// cursor advancing past the largest ID in each batch is the only thing that
// makes the loop terminate, so this pins it.
func TestRunStagedBackfillTerminatesWhenWorkIsNeverCompleted(t *testing.T) {
	db := setupTestDB(t)
	newStagedProbeTable(t, db)
	seedStagedItems(t, db, 7)

	gen := &fakeStagedGen{db: db, skipWrite: true}

	done := make(chan error, 1)
	go func() {
		done <- db.RunStagedBackfill(context.Background(), gen, 2, nil)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunStagedBackfill: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunStagedBackfill did not terminate; the cursor is not advancing past " +
			"rows the generator cannot complete")
	}

	if gen.computed != 7 {
		t.Errorf("computed %d items, want each of the 7 exactly once", gen.computed)
	}
}

func TestRunStagedBackfillReportsProgress(t *testing.T) {
	db := setupTestDB(t)
	newStagedProbeTable(t, db)
	seedStagedItems(t, db, 5)

	var samples [][2]int64
	gen := &fakeStagedGen{db: db}
	if err := db.RunStagedBackfill(
		context.Background(), gen, 2,
		func(done, total int64) { samples = append(samples, [2]int64{done, total}) },
	); err != nil {
		t.Fatalf("RunStagedBackfill: %v", err)
	}

	if len(samples) != 3 {
		t.Fatalf("got %d progress samples, want 3 (batches of 2 over 5 items)", len(samples))
	}
	for i, sample := range samples {
		if sample[1] != 5 {
			t.Errorf("sample %d reported total %d, want 5", i, sample[1])
		}
		if i > 0 && sample[0] <= samples[i-1][0] {
			t.Errorf("sample %d done=%d did not advance past %d", i, sample[0], samples[i-1][0])
		}
	}
	if last := samples[len(samples)-1][0]; last != 5 {
		t.Errorf("final done = %d, want 5", last)
	}
}

// A nil progress callback is the case every non-CLI caller takes, so it must
// not panic -- the same contract RunBackfill has.
func TestRunStagedBackfillToleratesNilProgress(t *testing.T) {
	db := setupTestDB(t)
	newStagedProbeTable(t, db)
	seedStagedItems(t, db, 3)

	if err := db.RunStagedBackfill(context.Background(), &fakeStagedGen{db: db}, 2, nil); err != nil {
		t.Fatalf("RunStagedBackfill: %v", err)
	}
	if got := countStagedProbeRows(t, db); got != 3 {
		t.Errorf("wrote %d rows, want 3", got)
	}
}

// Ctrl-C during a long embed run has to stop it, and stop it reporting why.
func TestRunStagedBackfillStopsOnCanceledContext(t *testing.T) {
	db := setupTestDB(t)
	newStagedProbeTable(t, db)
	seedStagedItems(t, db, 10)

	ctx, cancel := context.WithCancel(context.Background())
	gen := &fakeStagedGen{
		db:          db,
		computeHook: func() error { cancel(); return nil },
	}

	err := db.RunStagedBackfill(ctx, gen, 2, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunStagedBackfill error = %v, want context.Canceled", err)
	}
	// The batch already computed when cancellation landed may or may not be
	// committed, but the run must not have processed everything.
	if got := countStagedProbeRows(t, db); got == 10 {
		t.Error("the run completed every item despite cancellation")
	}
}

func TestRunStagedBackfillEmptyWorkSetIsANoOp(t *testing.T) {
	db := setupTestDB(t)
	newStagedProbeTable(t, db)

	gen := &fakeStagedGen{db: db}
	if err := db.RunStagedBackfill(context.Background(), gen, 2, nil); err != nil {
		t.Fatalf("RunStagedBackfill: %v", err)
	}
	if gen.batches != 0 {
		t.Errorf("Compute ran %d times with nothing to do, want 0", gen.batches)
	}
}
