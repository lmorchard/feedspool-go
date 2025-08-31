package database

import (
	"fmt"

	"github.com/sirupsen/logrus"
)

// getMigrations returns the database migration scripts.
func getMigrations() map[int]string {
	return map[int]string{
		1: `-- Initial schema migration (handled by InitSchema)`,
		2: `-- Add latest_item_date column (handled specially in ApplyMigration)`,
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

	maxVersion := 2 // We know we have migrations 1 and 2

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
			if err := db.ApplyMigration(version, "ALTER TABLE feeds ADD COLUMN latest_item_date DATETIME;"); err != nil {
				return err
			}
		} else {
			// Column exists, just record the migration
			if _, err := db.conn.Exec("INSERT INTO schema_migrations (version) VALUES (?)", version); err != nil {
				return fmt.Errorf("failed to record migration %d: %w", version, err)
			}
		}
	default:
		return fmt.Errorf("unknown migration version: %d", version)
	}

	return nil
}
