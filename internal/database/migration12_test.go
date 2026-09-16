package database

import (
	"strings"
	"testing"
)

// A fresh database gets item_embeddings from schema.sql, an upgraded one gets
// it from migration 12, and both must land in the same place -- the same
// arrangement item_text has with migration 11.
func TestMigration12TableExistsOnAFreshDatabase(t *testing.T) {
	db := setupTestDB(t)

	var name string
	if err := db.conn.QueryRow(
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'item_embeddings'`,
	).Scan(&name); err != nil {
		t.Fatalf("item_embeddings is missing from a freshly initialized schema: %v", err)
	}
}

func TestMigration12IsRegistered(t *testing.T) {
	if maxMigrationVersion != migrationVersion12 {
		t.Errorf("maxMigrationVersion = %d, want %d: a migration that is not the head "+
			"never runs on an existing database", maxMigrationVersion, migrationVersion12)
	}

	sql, ok := getMigrations()[migrationVersion12]
	if !ok {
		t.Fatal("getMigrations() has no entry for migration 12")
	}
	for _, fragment := range []string{
		"item_embeddings",
		"PRIMARY KEY (item_id, model_id)",
		"ON DELETE CASCADE",
		"idx_item_embeddings_model",
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("migration 12 SQL does not contain %q", fragment)
		}
	}

	if _, ok := migrationDescriptions()[migrationVersion12]; !ok {
		t.Error("migration 12 has no description; migration progress output would be blank")
	}
}

// Migration 12 must NOT backfill. Embedding needs network access and a
// configured provider, and IsInitialized runs migrations -- so a backfilling
// migration 12 would turn every `serve` startup into a network operation.
func TestMigration12DoesNotEmbed(t *testing.T) {
	sql := getMigrations()[migrationVersion12]
	for _, forbidden := range []string{"http", "embed_", "INSERT INTO item_embeddings"} {
		if strings.Contains(strings.ToLower(sql), strings.ToLower(forbidden)) {
			t.Errorf("migration 12 SQL mentions %q; it must create the table and stop, "+
				"leaving backfill to `feedspool embed`", forbidden)
		}
	}
}

// IF NOT EXISTS throughout is what lets an interrupted migration simply start
// over, so re-applying has to be free.
func TestMigration12IsIdempotent(t *testing.T) {
	db := setupTestDB(t)

	sql := getMigrations()[migrationVersion12]
	for i := range 3 {
		if _, err := db.conn.Exec(sql); err != nil {
			t.Fatalf("applying migration 12 for the %d%s time failed: %v", i+1, "th", err)
		}
	}

	// Still exactly one table and one index afterwards.
	var tables, indexes int
	if err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'item_embeddings'`,
	).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_item_embeddings_model'`,
	).Scan(&indexes); err != nil {
		t.Fatal(err)
	}
	if tables != 1 || indexes != 1 {
		t.Errorf("after three applications: %d tables, %d indexes; want 1 and 1", tables, indexes)
	}
}

// The date window rides migration 9's expression index, so migration 12 adds
// no index of its own for it. This pins that the index it does add is only the
// model lookup.
func TestMigration12AddsNoDateIndex(t *testing.T) {
	sql := getMigrations()[migrationVersion12]
	if strings.Contains(sql, "published_date") || strings.Contains(sql, "first_seen") {
		t.Error("migration 12 indexes a date column; the window is already served by " +
			"idx_items_effective_date from migration 9")
	}
}

// Schema and migration DDL are maintained as a matched pair, and drift between
// them means a fresh database and an upgraded one disagree.
func TestMigration12MatchesSchemaFile(t *testing.T) {
	db := setupTestDB(t)

	// Compare the actual table definition a fresh (schema.sql) database has
	// against one built only by applying migration 12.
	var fromSchema string
	if err := db.conn.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'item_embeddings'`,
	).Scan(&fromSchema); err != nil {
		t.Fatal(err)
	}

	other, _ := setupTestDBForMigrations(t)
	if _, err := other.conn.Exec(`CREATE TABLE items (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := other.conn.Exec(getMigrations()[migrationVersion12]); err != nil {
		t.Fatal(err)
	}
	var fromMigration string
	if err := other.conn.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'item_embeddings'`,
	).Scan(&fromMigration); err != nil {
		t.Fatal(err)
	}

	// normalizeSQL (expressions_test.go) collapses whitespace, so this
	// compares the definition rather than the indentation.
	if normalizeSQL(fromSchema) != normalizeSQL(fromMigration) {
		t.Errorf("item_embeddings differs between schema.sql and migration 12.\nschema.sql: %s\nmigration:  %s",
			normalizeSQL(fromSchema), normalizeSQL(fromMigration))
	}
}
