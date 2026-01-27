package database

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// setupTestDBForMigrations creates a database without running InitSchema
// so we can test the migration system from a clean state.
func setupTestDBForMigrations(t *testing.T) (db *DB, tempDir string) {
	t.Helper()

	// Create temporary database file
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "feedspool_migration_test.db")

	// Open database connection directly without initializing schema
	sqlDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}

	// Create DB instance manually
	db = &DB{conn: sqlDB}

	// Cleanup function
	t.Cleanup(func() {
		db.Close()
		os.Remove(dbPath)
	})

	return db, dbPath
}

// setupOldDatabase creates a database with the old schema (without latest_item_date column).
func setupOldDatabase(t *testing.T) *DB {
	t.Helper()

	db, _ := setupTestDBForMigrations(t)

	// Create the old schema manually (without latest_item_date column)
	oldSchema := `
		CREATE TABLE feeds (
			url TEXT PRIMARY KEY,
			title TEXT,
			description TEXT,
			last_updated DATETIME,
			etag TEXT,
			last_modified TEXT,
			feed_json TEXT
		);
		
		CREATE TABLE items (
			feed_url TEXT,
			guid TEXT,
			title TEXT,
			link TEXT,
			published_date DATETIME,
			content TEXT,
			summary TEXT,
			archived BOOLEAN DEFAULT FALSE,
			item_json TEXT,
			PRIMARY KEY (feed_url, guid),
			FOREIGN KEY (feed_url) REFERENCES feeds(url) ON DELETE CASCADE
		);
		
		CREATE INDEX idx_items_published_date ON items(published_date);
		CREATE INDEX idx_items_archived ON items(archived);
	`

	if _, err := db.conn.Exec(oldSchema); err != nil {
		t.Fatalf("Failed to create old schema: %v", err)
	}

	return db
}

func TestGetMigrationVersion(t *testing.T) {
	tests := []struct {
		name            string
		setupDB         func(t *testing.T) *DB
		expectedVersion int
		shouldError     bool
	}{
		{
			name: "fresh database with no migrations table",
			setupDB: func(t *testing.T) *DB {
				db, _ := setupTestDBForMigrations(t)
				return db
			},
			expectedVersion: 0,
			shouldError:     false,
		},
		{
			name: "database with empty migrations table",
			setupDB: func(t *testing.T) *DB {
				db, _ := setupTestDBForMigrations(t)
				// Create empty migrations table
				_, err := db.conn.Exec(`
					CREATE TABLE schema_migrations (
						version INTEGER PRIMARY KEY,
						applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
					)
				`)
				if err != nil {
					t.Fatal(err)
				}
				return db
			},
			expectedVersion: 0,
			shouldError:     false,
		},
		{
			name: "database with migration version 1",
			setupDB: func(t *testing.T) *DB {
				db, _ := setupTestDBForMigrations(t)
				// Create migrations table with version 1
				_, err := db.conn.Exec(`
					CREATE TABLE schema_migrations (
						version INTEGER PRIMARY KEY,
						applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
					);
					INSERT INTO schema_migrations (version) VALUES (1);
				`)
				if err != nil {
					t.Fatal(err)
				}
				return db
			},
			expectedVersion: 1,
			shouldError:     false,
		},
		{
			name: "database with multiple migrations",
			setupDB: func(t *testing.T) *DB {
				db, _ := setupTestDBForMigrations(t)
				// Create migrations table with multiple versions
				_, err := db.conn.Exec(`
					CREATE TABLE schema_migrations (
						version INTEGER PRIMARY KEY,
						applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
					);
					INSERT INTO schema_migrations (version) VALUES (1), (2);
				`)
				if err != nil {
					t.Fatal(err)
				}
				return db
			},
			expectedVersion: 2,
			shouldError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := tt.setupDB(t)

			version, err := db.GetMigrationVersion()

			if tt.shouldError && err == nil {
				t.Errorf("GetMigrationVersion() expected error but got none")
			}

			if !tt.shouldError && err != nil {
				t.Errorf("GetMigrationVersion() unexpected error: %v", err)
			}

			if version != tt.expectedVersion {
				t.Errorf("GetMigrationVersion() = %d, want %d", version, tt.expectedVersion)
			}
		})
	}
}

func TestRunMigrationsOnFreshDatabase(t *testing.T) {
	db, _ := setupTestDBForMigrations(t)

	// Run migrations on fresh database - this should fail since migration 1
	// is not handled by applySpecificMigration (it's handled by InitSchema)
	err := db.RunMigrations()
	if err == nil {
		t.Fatalf("RunMigrations() on fresh DB should fail because migration 1 is not implemented in applySpecificMigration")
	}

	// The error should mention unknown migration version 1
	expectedError := "unknown migration version: 1"
	if err.Error() != expectedError {
		t.Errorf("RunMigrations() error = %v, want %v", err, expectedError)
	}

	// Even though migration failed, migrations table should still be created
	var count int
	err = db.conn.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Errorf("schema_migrations table should exist even after migration failure, but found %d tables", count)
	}
}

func TestRunMigrationsOnProperlyInitializedDatabase(t *testing.T) {
	db, _ := setupTestDBForMigrations(t)

	// Initialize schema first (this is the proper way)
	err := db.InitSchema()
	if err != nil {
		t.Fatalf("InitSchema() error = %v", err)
	}

	// Now run migrations - should work fine and bring us to version 2
	err = db.RunMigrations()
	if err != nil {
		t.Fatalf("RunMigrations() after InitSchema error = %v", err)
	}

	// Check final version
	version, err := db.GetMigrationVersion()
	if err != nil {
		t.Fatalf("GetMigrationVersion() error = %v", err)
	}

	if version != 4 {
		t.Errorf("After InitSchema + RunMigrations, version should be 4, got %d", version)
	}

	// Verify we have the latest_item_date column
	var colCount int
	err = db.conn.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('feeds') 
		WHERE name = 'latest_item_date'
	`).Scan(&colCount)
	if err != nil {
		t.Fatal(err)
	}

	if colCount != 1 {
		t.Errorf("Should have latest_item_date column after full initialization, found %d", colCount)
	}

	// Verify we have the url_metadata table
	var tableCount int
	err = db.conn.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master 
		WHERE type='table' AND name='url_metadata'
	`).Scan(&tableCount)
	if err != nil {
		t.Fatal(err)
	}

	if tableCount != 1 {
		t.Errorf("Should have url_metadata table after full initialization, found %d", tableCount)
	}
}

func TestRunMigrationsOnExistingDatabase(t *testing.T) {
	db := setupOldDatabase(t)

	// Verify the old schema doesn't have latest_item_date column
	var colCount int
	err := db.conn.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('feeds') 
		WHERE name = 'latest_item_date'
	`).Scan(&colCount)
	if err != nil {
		t.Fatal(err)
	}

	if colCount != 0 {
		t.Errorf("Old database should not have latest_item_date column, but found %d", colCount)
	}

	// Run migrations
	err = db.RunMigrations()
	if err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}

	// Check that migration version is now 2
	version, err := db.GetMigrationVersion()
	if err != nil {
		t.Fatalf("GetMigrationVersion() error = %v", err)
	}

	if version != 4 {
		t.Errorf("After migration, version should be 4, got %d", version)
	}

	// Verify latest_item_date column was added
	err = db.conn.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('feeds') 
		WHERE name = 'latest_item_date'
	`).Scan(&colCount)
	if err != nil {
		t.Fatal(err)
	}

	if colCount != 1 {
		t.Errorf("After migration, should have latest_item_date column, but found %d", colCount)
	}

	// Verify we can insert data into the new column
	_, err = db.conn.Exec(`
		INSERT INTO feeds (url, title, latest_item_date) 
		VALUES ('https://test.com', 'Test', '2024-01-01 12:00:00')
	`)
	if err != nil {
		t.Errorf("Should be able to insert data with latest_item_date column: %v", err)
	}
}

func TestRunMigrationsIdempotent(t *testing.T) {
	db := setupOldDatabase(t)

	// Run migrations twice
	err := db.RunMigrations()
	if err != nil {
		t.Fatalf("First RunMigrations() error = %v", err)
	}

	err = db.RunMigrations()
	if err != nil {
		t.Fatalf("Second RunMigrations() error = %v", err)
	}

	// Version should still be 2
	version, err := db.GetMigrationVersion()
	if err != nil {
		t.Fatalf("GetMigrationVersion() error = %v", err)
	}

	if version != 4 {
		t.Errorf("After double migration, version should be 4, got %d", version)
	}

	// Should still have exactly one latest_item_date column
	var colCount int
	err = db.conn.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('feeds') 
		WHERE name = 'latest_item_date'
	`).Scan(&colCount)
	if err != nil {
		t.Fatal(err)
	}

	if colCount != 1 {
		t.Errorf("Should have exactly 1 latest_item_date column after double migration, found %d", colCount)
	}
}

func TestRunMigrationsWithExistingColumn(t *testing.T) {
	db, _ := setupTestDBForMigrations(t)

	// Create schema that already has the latest_item_date column
	schemaWithColumn := `
		CREATE TABLE feeds (
			url TEXT PRIMARY KEY,
			title TEXT,
			description TEXT,
			last_updated DATETIME,
			etag TEXT,
			last_modified TEXT,
			latest_item_date DATETIME,
			feed_json TEXT
		);
		
		CREATE TABLE items (
			feed_url TEXT,
			guid TEXT,
			title TEXT,
			link TEXT,
			published_date DATETIME,
			content TEXT,
			summary TEXT,
			archived BOOLEAN DEFAULT FALSE,
			item_json TEXT,
			PRIMARY KEY (feed_url, guid),
			FOREIGN KEY (feed_url) REFERENCES feeds(url) ON DELETE CASCADE
		);
	`

	if _, err := db.conn.Exec(schemaWithColumn); err != nil {
		t.Fatalf("Failed to create schema with existing column: %v", err)
	}

	// Run migrations - should detect existing column and just record the migration
	err := db.RunMigrations()
	if err != nil {
		t.Fatalf("RunMigrations() with existing column error = %v", err)
	}

	// Check that migration version is 2
	version, err := db.GetMigrationVersion()
	if err != nil {
		t.Fatalf("GetMigrationVersion() error = %v", err)
	}

	if version != 4 {
		t.Errorf("With existing column, version should be 4, got %d", version)
	}

	// Verify column still exists and works
	var colCount int
	err = db.conn.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('feeds') 
		WHERE name = 'latest_item_date'
	`).Scan(&colCount)
	if err != nil {
		t.Fatal(err)
	}

	if colCount != 1 {
		t.Errorf("Should still have latest_item_date column, found %d", colCount)
	}
}

func TestApplyMigration(t *testing.T) {
	db, _ := setupTestDBForMigrations(t)

	// Create migrations table first
	_, err := db.conn.Exec(`
		CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Create a test table to migrate
	_, err = db.conn.Exec(`
		CREATE TABLE test_table (
			id INTEGER PRIMARY KEY,
			name TEXT
		)
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Apply a test migration
	migrationSQL := "ALTER TABLE test_table ADD COLUMN description TEXT;"
	err = db.ApplyMigration(99, migrationSQL)
	if err != nil {
		t.Fatalf("ApplyMigration() error = %v", err)
	}

	// Verify column was added
	var colCount int
	err = db.conn.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('test_table') 
		WHERE name = 'description'
	`).Scan(&colCount)
	if err != nil {
		t.Fatal(err)
	}

	if colCount != 1 {
		t.Errorf("Migration should have added description column, found %d", colCount)
	}

	// Verify migration was recorded
	var recordedVersion int
	err = db.conn.QueryRow("SELECT version FROM schema_migrations WHERE version = 99").Scan(&recordedVersion)
	if err != nil {
		t.Fatalf("Migration should be recorded in schema_migrations: %v", err)
	}

	if recordedVersion != 99 {
		t.Errorf("Recorded migration version = %d, want 99", recordedVersion)
	}
}

func TestGetMigrations(t *testing.T) {
	migrations := getMigrations()

	// Check that we have the expected migrations
	if len(migrations) < 2 {
		t.Errorf("getMigrations() should return at least 2 migrations, got %d", len(migrations))
	}

	// Migration 1 is handled by InitSchema, not in getMigrations()
	if _, exists := migrations[1]; exists {
		t.Errorf("Migration 1 should not exist in getMigrations() (handled by InitSchema)")
	}

	// Check migration 2
	migration2, exists := migrations[2]
	if !exists {
		t.Errorf("Migration 2 should exist")
	}

	// Migration 2 should contain actual SQL, not just a comment
	expectedSQL := "ALTER TABLE feeds ADD COLUMN latest_item_date DATETIME;"
	if migration2 != expectedSQL {
		t.Errorf("Migration 2 SQL = %q, want %q", migration2, expectedSQL)
	}
}

func TestApplySpecificMigrationUsesMap(t *testing.T) {
	db := setupOldDatabase(t)

	// Create migrations table
	_, err := db.conn.Exec(`
		CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Apply migration 2 specifically
	err = db.applySpecificMigration(2)
	if err != nil {
		t.Fatalf("applySpecificMigration(2) error = %v", err)
	}

	// Verify the column was added (proving it used the SQL from the map)
	var colCount int
	err = db.conn.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('feeds') 
		WHERE name = 'latest_item_date'
	`).Scan(&colCount)
	if err != nil {
		t.Fatal(err)
	}

	if colCount != 1 {
		t.Errorf("applySpecificMigration should have added latest_item_date column, found %d", colCount)
	}

	// Verify migration was recorded
	version, err := db.GetMigrationVersion()
	if err != nil {
		t.Fatal(err)
	}

	if version != 2 {
		t.Errorf("Migration version should be 2 after applying migration 2, got %d", version)
	}
}
