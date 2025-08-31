package database

import (
	"fmt"

	"github.com/sirupsen/logrus"
)

// getMigrations returns the database migration scripts.
func getMigrations() map[int]string {
	return map[int]string{
		1: `-- Initial schema migration (handled by InitSchema)`,
		2: `ALTER TABLE feeds ADD COLUMN latest_item_date DATETIME;`,
		3: `CREATE TABLE IF NOT EXISTS url_metadata (
			url TEXT PRIMARY KEY,
			title TEXT,
			description TEXT,
			image_url TEXT,
			favicon_url TEXT,
			metadata JSON,
			last_fetch_at DATETIME,
			fetch_status_code INTEGER,
			fetch_error TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_url_metadata_url ON url_metadata(url);
		CREATE TRIGGER IF NOT EXISTS update_url_metadata_updated_at
		AFTER UPDATE ON url_metadata
		BEGIN
			UPDATE url_metadata SET updated_at = CURRENT_TIMESTAMP WHERE url = NEW.url;
		END;`,
	}
}

// RunMigrations applies any pending database migrations.
func (db *DB) RunMigrations() error {
	// Ensure schema_migrations table exists
	if _, err := db.conn.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	currentVersion, err := db.GetMigrationVersion()
	if err != nil {
		return fmt.Errorf("failed to get migration version: %w", err)
	}

	migrations := getMigrations()

	// If this is an existing database (currentVersion = 0) with tables, record initial schema version
	if currentVersion == 0 {
		var feedsTableExists int
		err := db.conn.QueryRow(
			"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='feeds'",
		).Scan(&feedsTableExists)
		if err == nil && feedsTableExists > 0 {
			// Record version 1 as applied for existing databases
			logrus.Info("Existing database detected, marking initial schema as version 1")
			if _, err := db.conn.Exec("INSERT INTO schema_migrations (version) VALUES (1)"); err != nil {
				return fmt.Errorf("failed to record initial schema version: %w", err)
			}
			currentVersion = 1
		}
	}

	maxVersion := 3 // We know we have migrations 1, 2, and 3

	// Check if any migrations are needed
	if currentVersion >= maxVersion {
		return nil // No migrations needed
	}

	logrus.Infof("Checking for database migrations (current version: %d)", currentVersion)

	appliedCount := 0
	for version := currentVersion + 1; version <= maxVersion; version++ {
		if _, exists := migrations[version]; exists {
			logrus.Infof("Applying migration %d: Adding latest_item_date column", version)
			if err := db.applySpecificMigration(version); err != nil {
				return err
			}
			appliedCount++
		}
	}

	if appliedCount > 0 {
		logrus.Infof("Successfully applied %d migration(s)", appliedCount)
	}

	return nil
}

// applySpecificMigration handles specific migrations with custom logic.
func (db *DB) applySpecificMigration(version int) error {
	switch version {
	case 2:
		// Check if latest_item_date column already exists
		var colCount int
		err := db.conn.QueryRow(`
			SELECT COUNT(*) FROM pragma_table_info('feeds') 
			WHERE name = 'latest_item_date'
		`).Scan(&colCount)
		if err != nil {
			return fmt.Errorf("failed to check column existence: %w", err)
		}

		if colCount == 0 {
			// Column doesn't exist, add it
			migrations := getMigrations()
			if err := db.ApplyMigration(version, migrations[version]); err != nil {
				return err
			}
		} else {
			// Column exists, just record the migration
			if _, err := db.conn.Exec("INSERT INTO schema_migrations (version) VALUES (?)", version); err != nil {
				return fmt.Errorf("failed to record migration %d: %w", version, err)
			}
		}
	case 3:
		// Check if url_metadata table already exists
		var tableCount int
		err := db.conn.QueryRow(`
			SELECT COUNT(*) FROM sqlite_master 
			WHERE type='table' AND name='url_metadata'
		`).Scan(&tableCount)
		if err != nil {
			return fmt.Errorf("failed to check table existence: %w", err)
		}

		// Apply migration regardless, as it uses IF NOT EXISTS
		migrations := getMigrations()
		if err := db.ApplyMigration(version, migrations[version]); err != nil {
			return err
		}
	default:
		// For any new migrations, just apply them directly
		migrations := getMigrations()
		if migration, exists := migrations[version]; exists {
			if err := db.ApplyMigration(version, migration); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("unknown migration version: %d", version)
		}
	}

	return nil
}
