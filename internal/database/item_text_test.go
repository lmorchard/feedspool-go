package database

import (
	"fmt"
	"testing"
	"time"

	"github.com/lmorchard/feedspool-go/internal/itemtext"
)

const (
	// ftsIntegrityCheckSQL asks FTS5 to compare its index against the content
	// table. An external-content index that has drifted returns wrong results
	// rather than erroring, so an explicit check is the only thing that catches
	// a missing trigger.
	//
	// The argument is load-bearing, and its sense is the opposite of what the
	// name suggests. Measured against this driver: with no argument, or with 0,
	// FTS5 checks only that the index is internally consistent -- which stays
	// true while the index and item_text disagree, so both an orphaned and a
	// missing entry sail through. Argument 1 is the one that reads the content
	// table back, and it reports SQLITE_CORRUPT for either.
	ftsIntegrityCheckSQL = `INSERT INTO items_fts(items_fts, rank) VALUES('integrity-check', 1)`

	// ftsMatchCountSQL counts index entries, not content rows. A bare
	// "SELECT COUNT(*) FROM items_fts" would read the content table and report
	// the same number whether or not the index was maintained, which would make
	// every trigger case below pass vacuously.
	ftsMatchCountSQL = `SELECT COUNT(*) FROM items_fts WHERE items_fts MATCH ?`

	// seedSharedTerm appears in the title of every seeded item, so a match count
	// on it is a count of indexed items.
	seedSharedTerm = "headline"

	// seedBodyTerm appears in the body of every seeded item.
	seedBodyTerm = "zeppelin"

	// replacementBodyTerm is the term an update swaps in for seedBodyTerm.
	replacementBodyTerm = "dirigible"

	// seedGUIDFormat builds the GUID of the nth seeded item.
	seedGUIDFormat = "seed-guid-%d"
)

// execSQL runs a statement that the test expects to succeed.
func execSQL(t *testing.T, db *DB, query string, args ...any) {
	t.Helper()
	if _, err := db.conn.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// integrityCheck fails the test if the FTS index disagrees with item_text.
func integrityCheck(t *testing.T, db *DB) {
	t.Helper()
	if _, err := db.conn.Exec(ftsIntegrityCheckSQL); err != nil {
		t.Fatalf("fts integrity-check failed: %v", err)
	}
}

// countMatching reports how many items the index returns for a bare term.
func countMatching(t *testing.T, db *DB, term string) int {
	t.Helper()
	var count int
	if err := db.conn.QueryRow(ftsMatchCountSQL, term).Scan(&count); err != nil {
		t.Fatalf("counting matches for %q: %v", term, err)
	}
	return count
}

// countIndexedItems reports how many seeded items the index still returns.
func countIndexedItems(t *testing.T, db *DB) int {
	t.Helper()
	return countMatching(t, db, seedSharedTerm)
}

// countItemTextRows reports how many derived-text rows exist.
func countItemTextRows(t *testing.T, db *DB) int {
	t.Helper()
	var count int
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM item_text`).Scan(&count); err != nil {
		t.Fatalf("counting item_text rows: %v", err)
	}
	return count
}

// countArchivedItems reports how many items are flagged archived.
func countArchivedItems(t *testing.T, db *DB) int {
	t.Helper()
	var count int
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM items WHERE archived = 1`).Scan(&count); err != nil {
		t.Fatalf("counting archived items: %v", err)
	}
	return count
}

// firstItemID returns the lowest item ID, which is the first seeded item.
func firstItemID(t *testing.T, db *DB) int64 {
	t.Helper()
	var id int64
	if err := db.conn.QueryRow(`SELECT MIN(id) FROM items`).Scan(&id); err != nil {
		t.Fatalf("reading first item id: %v", err)
	}
	return id
}

// seededItemContent is the raw HTML body seeded for item i.
func seededItemContent(index int, term string) string {
	return fmt.Sprintf("<div>Body of item %d, a %s</div>", index, term)
}

// seedSearchableItems inserts count items under fixtureFeedURL with
// deterministic, searchable text. published_date is always set:
// DeleteArchivedItems filters on the effective date being non-NULL, so an item
// without one would make the archived-purge case below assert nothing.
func seedSearchableItems(t *testing.T, count int) *DB {
	t.Helper()
	db := setupTestDB(t)
	if err := db.UpsertFeed(&Feed{URL: fixtureFeedURL}); err != nil {
		t.Fatalf("seeding feed: %v", err)
	}
	published := time.Now().UTC().Add(-time.Hour)
	for i := range count {
		item := &Item{
			FeedURL:       fixtureFeedURL,
			GUID:          fmt.Sprintf(seedGUIDFormat, i),
			Title:         fmt.Sprintf("Item %d %s", i, seedSharedTerm),
			Link:          fmt.Sprintf("https://example.com/item-%d", i),
			PublishedDate: published.Add(time.Duration(i) * time.Minute),
			Summary:       fmt.Sprintf("<p>Summary of item %d</p>", i),
			Content:       seededItemContent(i, seedBodyTerm),
		}
		if err := db.UpsertItem(item); err != nil {
			t.Fatalf("seeding item %d: %v", i, err)
		}
	}
	return db
}

// seedIndexedItems seeds items and brings the derived text and index up to date.
func seedIndexedItems(t *testing.T, count int) *DB {
	t.Helper()
	db := seedSearchableItems(t, count)
	if err := db.ReindexItemText(false, nil); err != nil {
		t.Fatalf("indexing seeded items: %v", err)
	}
	if got := countIndexedItems(t, db); got != count {
		t.Fatalf("seed indexed %d items, want %d", got, count)
	}
	return db
}

func TestItemTextTriggersCoverEveryDeletePath(t *testing.T) {
	const seedCount = 2
	cases := []struct {
		name     string
		act      func(t *testing.T, db *DB)
		wantRows int // expected items left in the index, from a seed of 2
	}{
		{"direct delete", func(t *testing.T, db *DB) {
			execSQL(t, db, `DELETE FROM item_text WHERE item_id = ?`, firstItemID(t, db))
		}, 1},
		{"one-level cascade from items", func(t *testing.T, db *DB) {
			execSQL(t, db, `DELETE FROM items WHERE id = ?`, firstItemID(t, db))
		}, 1},
		{"two-level cascade from feeds", func(t *testing.T, db *DB) {
			// feeds -> items -> item_text -> trigger. DeleteFeed reaches the
			// index no other way; there is no FTS code on the purge path.
			if err := db.DeleteFeed(fixtureFeedURL); err != nil {
				t.Fatal(err)
			}
		}, 0},
		{"archived purge", func(t *testing.T, db *DB) {
			execSQL(t, db, `UPDATE items SET archived = 1`)
			deleted, err := db.DeleteArchivedItems(time.Now().Add(time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			if deleted != seedCount {
				t.Fatalf("purge deleted %d items, want %d", deleted, seedCount)
			}
		}, 0},
		{"marking archived indexes nothing away", func(t *testing.T, db *DB) {
			// archived is a filter on items, not a text change, so the index
			// must be untouched.
			if err := db.MarkItemsArchived(fixtureFeedURL, nil); err != nil {
				t.Fatal(err)
			}
			// Confirm the call did something. Were MarkItemsArchived to become a
			// no-op, "the index is unchanged" would hold for the wrong reason.
			if got := countArchivedItems(t, db); got != seedCount {
				t.Fatalf("%d items are archived, want %d", got, seedCount)
			}
		}, seedCount},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			db := seedIndexedItems(t, seedCount)
			testCase.act(t, db)
			if got := countIndexedItems(t, db); got != testCase.wantRows {
				t.Errorf("index holds %d items, want %d", got, testCase.wantRows)
			}
			integrityCheck(t, db)
		})
	}
}

// TestItemTextUpdateRetiresStaleTerms guards the update trigger's 'delete'
// command. An update trigger that inserts without first deleting leaves both
// the old and the new body searchable, and integrity-check still passes, so
// only the stale-term assertion catches it.
func TestItemTextUpdateRetiresStaleTerms(t *testing.T) {
	db := seedIndexedItems(t, 1)
	if got := countMatching(t, db, seedBodyTerm); got != 1 {
		t.Fatalf("seeded body term matches %d items, want 1", got)
	}

	execSQL(
		t, db,
		`UPDATE items SET content = ? WHERE id = ?`,
		seededItemContent(0, replacementBodyTerm), firstItemID(t, db),
	)
	// Phase 3 owns the live write path; here a version rewind is what makes the
	// backfill recompute the row, which drives item_text through an UPDATE.
	execSQL(t, db, `UPDATE item_text SET generator_version = generator_version - 1`)
	if err := db.ReindexItemText(false, nil); err != nil {
		t.Fatal(err)
	}

	if got := countMatching(t, db, seedBodyTerm); got != 0 {
		t.Errorf("retired body term still matches %d items, want 0", got)
	}
	if got := countMatching(t, db, replacementBodyTerm); got != 1 {
		t.Errorf("new body term matches %d items, want 1", got)
	}
	if got := countIndexedItems(t, db); got != 1 {
		t.Errorf("untouched title term matches %d items, want 1", got)
	}
	integrityCheck(t, db)
}

func TestReindexItemTextRecomputesOnVersionBump(t *testing.T) {
	const seedCount = 3
	db := seedIndexedItems(t, seedCount)
	before := itemTextComputedAt(t, db)

	execSQL(t, db, `UPDATE item_text SET generator_version = ?`, itemtext.Version-1)
	if err := db.ReindexItemText(false, nil); err != nil {
		t.Fatal(err)
	}

	var stale int
	if err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM item_text WHERE generator <> ? OR generator_version <> ?`,
		itemtext.Generator, itemtext.Version,
	).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if stale != 0 {
		t.Errorf("%d rows still carry the old generator version", stale)
	}

	after := itemTextComputedAt(t, db)
	if len(after) != len(before) {
		t.Fatalf("row count changed from %d to %d", len(before), len(after))
	}
	for id, timestamp := range after {
		if timestamp == before[id] {
			t.Errorf("item %d kept computed_at %q, so it was not recomputed", id, timestamp)
		}
	}
	integrityCheck(t, db)
}

func TestReindexItemTextForceRebuilds(t *testing.T) {
	const (
		junkTerm  = "corrupted"
		seedCount = 3
	)
	db := seedIndexedItems(t, seedCount)

	// Corrupt the derived text by hand. Without force the backfill would leave
	// it alone: the rows are already at the current generator version.
	execSQL(t, db, `UPDATE item_text SET body = ?`, junkTerm)
	if got := countMatching(t, db, junkTerm); got != seedCount {
		t.Fatalf("corrupted body matches %d items, want %d", got, seedCount)
	}

	if err := db.ReindexItemText(true, nil); err != nil {
		t.Fatal(err)
	}

	if got := countMatching(t, db, junkTerm); got != 0 {
		t.Errorf("corrupted term still matches %d items, want 0", got)
	}
	if got := countMatching(t, db, seedBodyTerm); got != seedCount {
		t.Errorf("rebuilt body term matches %d items, want %d", got, seedCount)
	}
	want := itemtext.Derive("", "", seededItemContent(0, seedBodyTerm), itemtext.DefaultOptions()).Body
	var body string
	if err := db.conn.QueryRow(
		`SELECT body FROM item_text WHERE item_id = ?`, firstItemID(t, db),
	).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if body != want {
		t.Errorf("rebuilt body = %q, want %q", body, want)
	}
	integrityCheck(t, db)
}

// The live write path and itemTextStalenessCondition have to agree on what
// "current" means. The predicate treats a row written by a different generator
// as stale even at a matching version; UpsertItem has to as well, or a row left
// behind by a second generator (#30) would look current to every fetch and
// never be rewritten with the text this generator would derive.
func TestUpsertItemRederivesTextLeftByAnotherGenerator(t *testing.T) {
	const (
		junkTerm       = "corrupted"
		otherGenerator = "some-other-generator"
	)
	db := seedIndexedItems(t, 1)
	id := firstItemID(t, db)

	// The hash and version stay exactly as the write path last wrote them, so
	// only the generator name can make this row stale. The mangled body is what
	// makes a re-derivation observable.
	execSQL(
		t, db,
		`UPDATE item_text SET generator = ?, body = ? WHERE item_id = ?`,
		otherGenerator, junkTerm, id,
	)
	if got := countMatching(t, db, junkTerm); got != 1 {
		t.Fatalf("mangled body matches %d items, want 1", got)
	}

	// Re-fetching the feed writes the identical item back, which is the case
	// upsertItemTextIfChanged short-circuits.
	item, err := db.getItemByKey(fixtureFeedURL, fmt.Sprintf(seedGUIDFormat, 0))
	if err != nil {
		t.Fatal(err)
	}
	if item == nil {
		t.Fatal("seeded item is missing")
	}
	if err := db.UpsertItem(item); err != nil {
		t.Fatal(err)
	}

	var generator, body string
	if err := db.conn.QueryRow(
		`SELECT generator, body FROM item_text WHERE item_id = ?`, id,
	).Scan(&generator, &body); err != nil {
		t.Fatal(err)
	}
	if generator != itemtext.Generator {
		t.Errorf("generator = %q, want %q", generator, itemtext.Generator)
	}
	if got := countMatching(t, db, junkTerm); got != 0 {
		t.Errorf("mangled term still matches %d items, want 0", got)
	}
	if got := countMatching(t, db, seedBodyTerm); got != 1 {
		t.Errorf("re-derived body term matches %d items, want 1", got)
	}
	integrityCheck(t, db)
}

// TestMigration11BackfillsExistingItems walks the path a real spool takes:
// items already on disk, no derived text, no index.
func TestMigration11BackfillsExistingItems(t *testing.T) {
	const seedCount = 3
	db := seedSearchableItems(t, seedCount)
	rewindPastMigration11(t, db)

	if err := db.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}

	version, err := db.GetMigrationVersion()
	if err != nil {
		t.Fatal(err)
	}
	if version != maxMigrationVersion {
		t.Errorf("migration version = %d, want %d", version, maxMigrationVersion)
	}
	if got := countItemTextRows(t, db); got != seedCount {
		t.Errorf("item_text has %d rows, want %d", got, seedCount)
	}
	if got := countIndexedItems(t, db); got != seedCount {
		t.Errorf("index holds %d items, want %d", got, seedCount)
	}
	integrityCheck(t, db)
}

// TestMigration11SchemaStageDoesNotRecordVersion pins half of the stage
// ordering: the DDL stage on its own must never record the schema version,
// because the version is what says "fully indexed". Re-running the full
// migration over the schema stage 1 already created also exercises the DDL
// idempotency an interrupted run depends on.
//
// The other half -- that a backfill which fails leaves the version unrecorded
// -- is TestMigration11LeavesVersionUnrecordedWhenIndexingFails below.
func TestMigration11SchemaStageDoesNotRecordVersion(t *testing.T) {
	const seedCount = 2
	db := seedSearchableItems(t, seedCount)
	rewindPastMigration11(t, db)

	// Stage 1 alone.
	if err := db.applyMigration11Schema(); err != nil {
		t.Fatalf("applyMigration11Schema() error = %v", err)
	}
	for _, name := range []string{"item_text", "items_fts"} {
		if !tableExists(t, db, name) {
			t.Errorf("%s does not exist after the schema stage", name)
		}
	}
	version, err := db.GetMigrationVersion()
	if err != nil {
		t.Fatal(err)
	}
	if version != migrationVersion10 {
		t.Errorf("version = %d after the schema stage, want %d -- the DDL stage must not record it",
			version, migrationVersion10)
	}
	if got := countItemTextRows(t, db); got != 0 {
		t.Errorf("item_text has %d rows after the schema stage, want 0", got)
	}

	// The full migration over that same schema: idempotent DDL, then backfill,
	// then the version.
	if err := db.applyMigration11(); err != nil {
		t.Fatalf("applyMigration11() error = %v", err)
	}
	version, err = db.GetMigrationVersion()
	if err != nil {
		t.Fatal(err)
	}
	if version != migrationVersion11 {
		t.Errorf("version = %d after the full migration, want %d", version, migrationVersion11)
	}
	if got := countItemTextRows(t, db); got != seedCount {
		t.Errorf("item_text has %d rows, want %d", got, seedCount)
	}
	if got := countIndexedItems(t, db); got != seedCount {
		t.Errorf("index holds %d items, want %d", got, seedCount)
	}
	integrityCheck(t, db)
}

// Two processes can enter migration 11 at once: IsInitialized runs migrations,
// so a running `serve` and a cron `fetch` both do on the first post-upgrade
// open, and this migration's window is tens of seconds rather than one fast
// transaction. Both do the work idempotently, and the loser then has nothing
// left to do but record the version -- which must not fail as a duplicate.
//
// A second in-process run stands in for the loser: identical DDL, a backfill
// with nothing left to do, and a version row that already exists. It does not
// reproduce the interleaving, but the duplicate version record is the only part
// of that race that ever failed.
func TestMigration11RecordsItsVersionIdempotently(t *testing.T) {
	const seedCount = 2
	db := seedSearchableItems(t, seedCount)
	rewindPastMigration11(t, db)

	if err := db.applyMigration11(); err != nil {
		t.Fatalf("first applyMigration11() error = %v", err)
	}
	if err := db.applyMigration11(); err != nil {
		t.Fatalf("second applyMigration11() error = %v", err)
	}

	version, err := db.GetMigrationVersion()
	if err != nil {
		t.Fatal(err)
	}
	if version != migrationVersion11 {
		t.Errorf("version = %d, want %d", version, migrationVersion11)
	}
	var records int
	if err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, migrationVersion11,
	).Scan(&records); err != nil {
		t.Fatal(err)
	}
	if records != 1 {
		t.Errorf("schema_migrations holds %d rows for version %d, want 1", records, migrationVersion11)
	}
	if got := countItemTextRows(t, db); got != seedCount {
		t.Errorf("item_text has %d rows, want %d", got, seedCount)
	}
	if got := countIndexedItems(t, db); got != seedCount {
		t.Errorf("index holds %d items, want %d", got, seedCount)
	}
	integrityCheck(t, db)
}

// TestMigration11LeavesVersionUnrecordedWhenIndexingFails is the one that
// matters: if the backfill does not finish, the migration must not be marked
// done, or search silently misses those items forever.
//
// The failure is induced without any production seam. A BEFORE INSERT trigger
// that raises ABORT makes upsertItemTextTx fail, which is a real error arriving
// by the real path -- no injection point, no test-only branch in the migration.
//
// It does not simulate a process killed mid-batch; nothing short of an actual
// crash does. But it does cover the property that matters operationally, which
// the schema-stage test alone cannot: a migration whose indexing stage returns
// an error leaves the version unbumped, and the next run finishes the job.
func TestMigration11LeavesVersionUnrecordedWhenIndexingFails(t *testing.T) {
	const seedCount = 2
	db := seedSearchableItems(t, seedCount)
	rewindPastMigration11(t, db)

	// The schema has to exist before the abort trigger can be attached to it.
	if err := db.applyMigration11Schema(); err != nil {
		t.Fatal(err)
	}
	execSQL(t, db, `CREATE TRIGGER item_text_abort BEFORE INSERT ON item_text
		BEGIN SELECT RAISE(ABORT, 'induced indexing failure'); END`)

	if err := db.applyMigration11(); err == nil {
		t.Fatal("applyMigration11() succeeded, want the induced indexing failure")
	}
	version, err := db.GetMigrationVersion()
	if err != nil {
		t.Fatal(err)
	}
	if version == migrationVersion11 {
		t.Fatalf("version = %d after a failed backfill; a half-indexed database must not be marked done",
			migrationVersion11)
	}
	if got := countItemTextRows(t, db); got != 0 {
		t.Errorf("item_text has %d rows after the aborted backfill, want 0", got)
	}

	// Resuming: the next run re-applies the idempotent DDL and finishes.
	execSQL(t, db, `DROP TRIGGER item_text_abort`)
	if err := db.applyMigration11(); err != nil {
		t.Fatalf("resumed applyMigration11() error = %v", err)
	}
	version, err = db.GetMigrationVersion()
	if err != nil {
		t.Fatal(err)
	}
	if version != migrationVersion11 {
		t.Errorf("version = %d after the resumed migration, want %d", version, migrationVersion11)
	}
	if got := countIndexedItems(t, db); got != seedCount {
		t.Errorf("index holds %d items, want %d", got, seedCount)
	}
	integrityCheck(t, db)
}

// tableExists reports whether a table or virtual table is present.
func tableExists(t *testing.T, db *DB, name string) bool {
	t.Helper()
	var count int
	if err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name,
	).Scan(&count); err != nil {
		t.Fatalf("checking for table %s: %v", name, err)
	}
	return count > 0
}

// rewindPastMigration11 drops everything migration 11 creates, leaving a
// database that looks like it was last opened by an older feedspool.
func rewindPastMigration11(t *testing.T, db *DB) {
	t.Helper()
	for _, statement := range []string{
		`DROP TRIGGER IF EXISTS item_text_ai`,
		`DROP TRIGGER IF EXISTS item_text_ad`,
		`DROP TRIGGER IF EXISTS item_text_au`,
		`DROP TABLE IF EXISTS items_fts`,
		`DROP TABLE IF EXISTS item_text`,
	} {
		execSQL(t, db, statement)
	}
	execSQL(t, db, `DELETE FROM schema_migrations WHERE version = ?`, migrationVersion11)
}

// itemTextComputedAt maps each derived row to its computed_at timestamp.
func itemTextComputedAt(t *testing.T, db *DB) map[int64]string {
	t.Helper()
	rows, err := db.conn.Query(`SELECT item_id, computed_at FROM item_text`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	stamps := make(map[int64]string)
	for rows.Next() {
		var id int64
		var computedAt string
		if err := rows.Scan(&id, &computedAt); err != nil {
			t.Fatal(err)
		}
		stamps[id] = computedAt
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return stamps
}
