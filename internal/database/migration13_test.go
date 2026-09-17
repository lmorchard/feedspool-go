package database

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	tblTopicRuns  = "topic_runs"
	tblTopics     = "topics"
	tblTopicItems = "topic_items"
)

func TestMigration13TableExistsOnAFreshDatabase(t *testing.T) {
	db := setupTestDB(t)

	tables := []string{tblTopicRuns, tblTopics, tblTopicItems}
	for _, table := range tables {
		var name string
		if err := db.conn.QueryRow(
			`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table,
		).Scan(&name); err != nil {
			t.Fatalf("%s is missing from a freshly initialized schema: %v", table, err)
		}
	}
}

func TestMigration13IsRegistered(t *testing.T) {
	if maxMigrationVersion != migrationVersion13 {
		t.Errorf("maxMigrationVersion = %d, want %d", maxMigrationVersion, migrationVersion13)
	}

	sql, ok := getMigrations()[migrationVersion13]
	if !ok {
		t.Fatal("getMigrations() has no entry for migration 13")
	}
	for _, fragment := range []string{
		tblTopicRuns,
		tblTopics,
		tblTopicItems,
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("migration 13 SQL does not contain %q", fragment)
		}
	}

	if _, ok := migrationDescriptions()[migrationVersion13]; !ok {
		t.Error("migration 13 has no description")
	}
}

func TestMigration13IsIdempotent(t *testing.T) {
	db := setupTestDB(t)

	sql := getMigrations()[migrationVersion13]
	for i := range 3 {
		if _, err := db.conn.Exec(sql); err != nil {
			t.Fatalf("applying migration 13 for the %d%s time failed: %v", i+1, "th", err)
		}
	}

	for _, table := range []string{tblTopicRuns, tblTopics, tblTopicItems} {
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

func TestMigration13MatchesSchemaFile(t *testing.T) {
	db := setupTestDB(t)

	other, _ := setupTestDBForMigrations(t)
	if _, err := other.conn.Exec(`CREATE TABLE items (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := other.conn.Exec(getMigrations()[migrationVersion13]); err != nil {
		t.Fatal(err)
	}

	tables := []string{tblTopicRuns, tblTopics, tblTopicItems}
	for _, table := range tables {
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
			t.Errorf("%s differs between schema.sql and migration 13.\nschema.sql: %s\nmigration:  %s",
				table, normalizeSQL(fromSchema), normalizeSQL(fromMigration))
		}
	}
}

func TestMigration13TopicStorage(t *testing.T) {
	db := setupTestDB(t)

	now := time.Now().Round(time.Second).UTC()
	start := now.Add(-48 * time.Hour)
	end := now

	res, err := db.conn.Exec(`
		INSERT INTO topic_runs (created_at, window_start, window_end, embed_model_id, llm_model_id)
		VALUES (?, ?, ?, ?, ?)
	`, formatDatabaseTime(now), formatDatabaseTime(start), formatDatabaseTime(end), "nomic", "qwen")
	require.NoError(t, err)

	runID, err := res.LastInsertId()
	require.NoError(t, err)

	res, err = db.conn.Exec(`
		INSERT INTO topics (run_id, label, score)
		VALUES (?, ?, ?)
	`, runID, "Tech News", 5.0)
	require.NoError(t, err)

	topicID, err := res.LastInsertId()
	require.NoError(t, err)

	_, err = db.conn.Exec(`INSERT INTO feeds (url) VALUES (?)`, "http://example.com/feed")
	require.NoError(t, err)
	res, err = db.conn.Exec(`INSERT INTO items (feed_url, guid, title, link) VALUES (?, ?, ?, ?)`,
		"http://example.com/feed", "guid1", "Title 1", "http://example.com/1")
	require.NoError(t, err)
	itemID, err := res.LastInsertId()
	require.NoError(t, err)

	_, err = db.conn.Exec(`
		INSERT INTO topic_items (topic_id, item_id)
		VALUES (?, ?)
	`, topicID, itemID)
	require.NoError(t, err)

	var fetchedLabel string
	var fetchedScore float64
	err = db.conn.QueryRowContext(context.Background(), `
		SELECT label, score FROM topics WHERE id = ?
	`, topicID).Scan(&fetchedLabel, &fetchedScore)
	require.NoError(t, err)

	assert.Equal(t, "Tech News", fetchedLabel)
	assert.Equal(t, 5.0, fetchedScore)
}
