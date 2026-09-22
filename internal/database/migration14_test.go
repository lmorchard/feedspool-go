package database

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lmorchard/feedspool-go/internal/lineage"
)

const (
	tblTopicThreads = "topic_threads"
	tblTopicLineage = "topic_lineage"
)

// Labels for the backfill fixture: letter = story, digit = run.
const (
	lblA1, lblA2, lblA3 = "A1", "A2", "A3"
	lblB1, lblB2        = "B1", "B2"
	lblC3               = "C3"
)

func TestMigration14TableExistsOnAFreshDatabase(t *testing.T) {
	db := setupTestDB(t)

	for _, table := range []string{tblTopicThreads, tblTopicLineage} {
		var name string
		if err := db.conn.QueryRow(
			`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table,
		).Scan(&name); err != nil {
			t.Fatalf("%s is missing from a freshly initialized schema: %v", table, err)
		}
	}
}

func TestMigration14IsRegistered(t *testing.T) {
	if maxMigrationVersion != migrationVersion14 {
		t.Errorf("maxMigrationVersion = %d, want %d", maxMigrationVersion, migrationVersion14)
	}

	sql, ok := getMigrations()[migrationVersion14]
	if !ok {
		t.Fatal("getMigrations() has no entry for migration 14")
	}
	for _, fragment := range []string{tblTopicThreads, tblTopicLineage, "idx_topic_lineage_thread", "idx_topic_lineage_hash"} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("migration 14 SQL does not contain %q", fragment)
		}
	}

	if _, ok := migrationDescriptions()[migrationVersion14]; !ok {
		t.Error("migration 14 has no description")
	}
}

func TestMigration14IsIdempotent(t *testing.T) {
	db := setupTestDB(t)

	sql := getMigrations()[migrationVersion14]
	for i := range 3 {
		if _, err := db.conn.Exec(sql); err != nil {
			t.Fatalf("applying migration 14 for the %d%s time failed: %v", i+1, "th", err)
		}
	}

	for _, table := range []string{tblTopicThreads, tblTopicLineage} {
		var count int
		if err := db.conn.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table,
		).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("after three applications: %d tables for %s; want 1", count, table)
		}
	}
}

func TestMigration14MatchesSchemaFile(t *testing.T) {
	db := setupTestDB(t)

	other, _ := setupTestDBForMigrations(t)
	if _, err := other.conn.Exec(`CREATE TABLE items (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := other.conn.Exec(getMigrations()[migrationVersion13]); err != nil {
		t.Fatal(err)
	}
	if _, err := other.conn.Exec(getMigrations()[migrationVersion14]); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{tblTopicThreads, tblTopicLineage} {
		var fromSchema string
		if err := db.conn.QueryRow(
			`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table,
		).Scan(&fromSchema); err != nil {
			t.Fatal(err)
		}

		var fromMigration string
		if err := other.conn.QueryRow(
			`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table,
		).Scan(&fromMigration); err != nil {
			t.Fatal(err)
		}

		if normalizeSQL(fromSchema) != normalizeSQL(fromMigration) {
			t.Errorf("%s differs between schema.sql and migration 14.\nschema.sql: %s\nmigration:  %s",
				table, normalizeSQL(fromSchema), normalizeSQL(fromMigration))
		}
	}
}

// The backfill added in a later phase runs in Go against local rows only. The
// DDL itself must stay free of anything that looks like a network call or a
// data write, so a fresh `serve` startup cannot become a network operation.
func TestMigration14DoesNotTouchNetwork(t *testing.T) {
	sql := getMigrations()[migrationVersion14]
	for _, forbidden := range []string{"://", "INSERT"} {
		if strings.Contains(sql, forbidden) {
			t.Errorf("migration 14 DDL contains %q", forbidden)
		}
	}
}

// rewindPastMigration14 drops what migration 14 creates so RunMigrations will
// apply it again. `>= 14` for the same reason rewindPastMigration11 uses `>=`:
// GetMigrationVersion reads MAX(version).
func rewindPastMigration14(t *testing.T, db *DB) {
	t.Helper()
	execSQL(t, db, `DROP TABLE IF EXISTS topic_lineage`)
	execSQL(t, db, `DROP TABLE IF EXISTS topic_threads`)
	execSQL(t, db, `DELETE FROM schema_migrations WHERE version >= ?`, migrationVersion14)
}

// legacyRun writes a run and its topics the way a pre-migration-14 feedspool
// did: no threads, no lineage. topics is label -> item IDs; scores are sizes.
func legacyRun(t *testing.T, db *DB, at time.Time, topics map[string][]int64) (runID int64, topicIDs map[string]int64) {
	t.Helper()
	res, err := db.conn.Exec(`
		INSERT INTO topic_runs (created_at, window_start, window_end, embed_model_id, llm_model_id)
		VALUES (?, ?, ?, ?, ?)`,
		formatDatabaseTime(at), formatDatabaseTime(at.Add(-24*time.Hour)), formatDatabaseTime(at), "nomic", "qwen")
	if err != nil {
		t.Fatal(err)
	}
	runID, _ = res.LastInsertId()
	topicIDs = make(map[string]int64, len(topics))
	for label, items := range topics {
		res, err := db.conn.Exec(`INSERT INTO topics (run_id, label, score) VALUES (?, ?, ?)`,
			runID, label, float64(len(items)))
		if err != nil {
			t.Fatal(err)
		}
		topicID, _ := res.LastInsertId()
		topicIDs[label] = topicID
		for _, item := range items {
			execSQL(t, db, `INSERT INTO topic_items (topic_id, item_id) VALUES (?, ?)`, topicID, item)
		}
	}
	return runID, topicIDs
}

func threadOf(t *testing.T, db *DB, topicID int64) int64 {
	t.Helper()
	var threadID int64
	if err := db.conn.QueryRow(`SELECT thread_id FROM topic_lineage WHERE topic_id = ?`, topicID).Scan(&threadID); err != nil {
		t.Fatalf("topic %d has no lineage row: %v", topicID, err)
	}
	return threadID
}

func scalar(t *testing.T, db *DB, query string, args ...any) string {
	t.Helper()
	var out string
	if err := db.conn.QueryRow(query, args...).Scan(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestMigration14BackfillsExistingRuns(t *testing.T) {
	db := setupTestDB(t)
	rewindPastMigration14(t, db)

	ids := make([]int64, 9)
	for i := range ids {
		ids[i] = seedItem(t, db, "item-"+string(rune('a'+i)), time.Now())
	}
	base := time.Now().UTC().Add(-3 * time.Hour)
	r1, t1 := legacyRun(t, db, base, map[string][]int64{
		lblA1: {ids[0], ids[1], ids[2]},
		lblB1: {ids[3], ids[4]},
	})
	_, t2 := legacyRun(t, db, base.Add(time.Hour), map[string][]int64{
		lblA2: {ids[0], ids[1], ids[2]},
		lblB2: {ids[3], ids[4], ids[5]},
	})
	_, t3 := legacyRun(t, db, base.Add(2*time.Hour), map[string][]int64{
		lblA3: {ids[0], ids[1], ids[2]},
		lblC3: {ids[6], ids[7]},
	})
	_ = r1

	if err := db.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	if v, _ := db.GetMigrationVersion(); v != migrationVersion14 {
		t.Fatalf("migration version = %d, want %d", v, migrationVersion14)
	}

	// A: identical membership across all three runs -> one thread, latest label.
	threadA := threadOf(t, db, t1[lblA1])
	if threadOf(t, db, t2[lblA2]) != threadA || threadOf(t, db, t3[lblA3]) != threadA {
		t.Error("the three A topics do not share a thread")
	}
	if got := scalar(t, db, `SELECT label FROM topic_threads WHERE id = ?`, threadA); got != lblA3 {
		t.Errorf("thread A label = %q, want the latest run's %q", got, lblA3)
	}
	if got := scalar(t, db, `SELECT labeled_at FROM topic_threads WHERE id = ?`, threadA); got != formatDatabaseTime(base.Add(2*time.Hour)) {
		t.Errorf("thread A labeled_at = %s, want run 3's time", got)
	}
	if got := scalar(t, db, `SELECT first_seen_at FROM topic_threads WHERE id = ?`, threadA); got != formatDatabaseTime(base) {
		t.Errorf("thread A first_seen_at = %s, want run 1's time", got)
	}
	if got := scalar(t, db, `SELECT last_seen_at FROM topic_threads WHERE id = ?`, threadA); got != formatDatabaseTime(base.Add(2*time.Hour)) {
		t.Errorf("thread A last_seen_at = %s, want run 3's time", got)
	}

	// B: {3,4} -> {3,4,5} is Jaccard 2/3, above the attach threshold -> same thread.
	threadB := threadOf(t, db, t1[lblB1])
	if threadOf(t, db, t2[lblB2]) != threadB {
		t.Error("B1 and B2 should share a thread (Jaccard 0.67)")
	}
	if got := scalar(t, db, `SELECT label FROM topic_threads WHERE id = ?`, threadB); got != lblB2 {
		t.Errorf("thread B label = %q, want %q", got, lblB2)
	}

	// C: nothing like it before -> its own thread.
	threadC := threadOf(t, db, t3[lblC3])
	if threadC == threadA || threadC == threadB {
		t.Error("C3 should have opened a new thread")
	}

	if n := scalar(t, db, `SELECT COUNT(*) FROM topic_threads`); n != "3" {
		t.Errorf("thread count = %s, want 3", n)
	}
	if n := scalar(t, db, `SELECT COUNT(*) FROM topic_lineage`); n != "6" {
		t.Errorf("lineage rows = %s, want 6", n)
	}
	if n := scalar(t, db, `SELECT COUNT(*) FROM topic_lineage WHERE label_source <> 'generated'`); n != "0" {
		t.Errorf("%s backfilled rows are not 'generated'", n)
	}
	if n := scalar(t, db, `SELECT COUNT(*) FROM topic_lineage WHERE length(set_hash) <> 32`); n != "0" {
		t.Errorf("%s backfilled rows have a hash that is not 32 chars", n)
	}
}

func TestMigration14BackfillIsIdempotent(t *testing.T) {
	db := setupTestDB(t)
	rewindPastMigration14(t, db)
	id1 := seedItem(t, db, "item-1", time.Now())
	id2 := seedItem(t, db, "item-2", time.Now())
	base := time.Now().UTC().Add(-2 * time.Hour)
	legacyRun(t, db, base, map[string][]int64{lblA1: {id1, id2}})
	legacyRun(t, db, base.Add(time.Hour), map[string][]int64{lblA2: {id1, id2}})
	if err := db.RunMigrations(); err != nil {
		t.Fatal(err)
	}
	before := scalar(t, db, `SELECT COUNT(*) || '/' || (SELECT COUNT(*) FROM topic_lineage) || '/' ||
		(SELECT MAX(labeled_at) FROM topic_threads) FROM topic_threads`)

	if err := db.BackfillTopicLineage(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	after := scalar(t, db, `SELECT COUNT(*) || '/' || (SELECT COUNT(*) FROM topic_lineage) || '/' ||
		(SELECT MAX(labeled_at) FROM topic_threads) FROM topic_threads`)
	if before != after {
		t.Errorf("second backfill changed state: %s -> %s", before, after)
	}
	if before[:2] != "1/" {
		t.Errorf("expected a single thread, got %s", before)
	}
}

func TestMigration14BackfillLeavesLiveLineageAlone(t *testing.T) {
	db := setupTestDB(t)
	id1 := seedItem(t, db, "item-1", time.Now())
	topic := &Topic{Label: "Live", Score: 1.0}
	run := &TopicRun{
		CreatedAt: time.Now().UTC(), WindowStart: time.Now().UTC(), WindowEnd: time.Now().UTC(),
		EmbedModelID: "nomic", LLMModelID: "qwen",
	}
	if err := db.InsertTopicRun(context.Background(), run, []*Topic{topic}, map[*Topic][]int64{topic: {id1}}); err != nil {
		t.Fatal(err)
	}
	before := scalar(t, db, `SELECT thread_id || '/' || set_hash || '/' || label_source FROM topic_lineage WHERE topic_id = ?`, topic.ID)

	if err := db.BackfillTopicLineage(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	after := scalar(t, db, `SELECT thread_id || '/' || set_hash || '/' || label_source FROM topic_lineage WHERE topic_id = ?`, topic.ID)
	if before != after {
		t.Errorf("backfill rewrote live lineage: %s -> %s", before, after)
	}
	if n := scalar(t, db, `SELECT COUNT(*) FROM topic_threads`); n != "1" {
		t.Errorf("thread count = %s, want 1", n)
	}
}

func TestMigration14BackfillOnEmptyDatabase(t *testing.T) {
	db := setupTestDB(t)
	calls := 0
	if err := db.BackfillTopicLineage(context.Background(), func(_, _ int64) { calls++ }); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Errorf("progress reported %d times with no runs", calls)
	}
	if n := scalar(t, db, `SELECT COUNT(*) FROM topic_threads`); n != "0" {
		t.Errorf("thread count = %s, want 0", n)
	}
}

// The backfill must thread history by the configured rule, not the defaults,
// or the first live run disagrees with the migration about what a thread is.
func TestMigration14BackfillHonorsConfiguredRule(t *testing.T) {
	db := setupTestDB(t)
	rewindPastMigration14(t, db)

	ids := make([]int64, 6)
	for i := range ids {
		ids[i] = seedItem(t, db, "item-"+string(rune('a'+i)), time.Now())
	}
	base := time.Now().UTC().Add(-3 * time.Hour)
	_, t1 := legacyRun(t, db, base, map[string][]int64{
		lblB1: {ids[0], ids[1]},
		lblA1: {ids[3], ids[4], ids[5]},
	})
	_, t2 := legacyRun(t, db, base.Add(time.Hour), map[string][]int64{
		lblB2: {ids[0], ids[1], ids[2]},
	})
	_, t3 := legacyRun(t, db, base.Add(2*time.Hour), map[string][]int64{
		lblA3: {ids[3], ids[4], ids[5]},
	})

	db.SetLineageOptions(lineage.Options{AttachThreshold: 0.8, InheritThreshold: 0.9, Inherit: true}, 1)
	if err := db.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	// B1 -> B2 is Jaccard 2/3: joins at the default 0.5, not at the configured 0.8.
	if threadOf(t, db, t1[lblB1]) == threadOf(t, db, t2[lblB2]) {
		t.Error("B2 joined B1's thread despite a configured attach threshold of 0.8")
	}
	// A skipped run 2; a lookback of 1 cannot see run 1, so A3 opens a new thread.
	if threadOf(t, db, t1[lblA1]) == threadOf(t, db, t3[lblA3]) {
		t.Error("A3 joined A1's thread despite a configured lookback of 1")
	}
	// Inherit is forced off for the replay regardless of configuration.
	if n := scalar(t, db, `SELECT COUNT(*) FROM topic_lineage WHERE label_source = 'inherited'`); n != "0" {
		t.Errorf("%s backfilled rows are marked inherited", n)
	}
	if n := scalar(t, db, `SELECT COUNT(*) FROM topic_threads`); n != "4" {
		t.Errorf("thread count = %s, want 4", n)
	}
}
