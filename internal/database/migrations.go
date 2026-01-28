package database

import (
	"fmt"

	"github.com/sirupsen/logrus"
)

const (
	// Migration version constants.
	migrationVersion1   = 1 // Initial schema (handled by InitSchema)
	migrationVersion2   = 2 // Add latest_item_date column to feeds
	migrationVersion3   = 3 // Add url_metadata table
	migrationVersion4   = 4 // Add first_seen column to items
	maxMigrationVersion = migrationVersion4
)

// getMigrations returns the database migration scripts.
func getMigrations() map[int]string {
	return map[int]string{
		// Migration 1 is handled by InitSchema, not listed here
		migrationVersion2: `ALTER TABLE feeds ADD COLUMN latest_item_date DATETIME;`,
		migrationVersion3: `CREATE TABLE IF NOT EXISTS url_metadata (
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
		migrationVersion4: `ALTER TABLE items ADD COLUMN first_seen DATETIME;`,
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
			if _, err := db.conn.Exec("INSERT INTO schema_migrations (version) VALUES (?)", migrationVersion1); err != nil {
				return fmt.Errorf("failed to record initial schema version: %w", err)
			}
			currentVersion = migrationVersion1
		}
	}

	// Check if any migrations are needed
	if currentVersion >= maxMigrationVersion {
		return nil // No migrations needed
	}

	logrus.Infof("Checking for database migrations (current version: %d)", currentVersion)

	appliedCount := 0
	for version := currentVersion + 1; version <= maxMigrationVersion; version++ {
		if _, exists := migrations[version]; exists {
			logrus.Infof("Applying migration %d: Adding latest_item_date column", version)
			if err := db.applySpecificMigration(version); err != nil {
				return err
			}
			appliedCount++
		} else {
			// Migration doesn't exist - this is an error
			return fmt.Errorf("unknown migration version: %d", version)
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
	case migrationVersion2:
		return db.applyMigration2()
	case migrationVersion3:
		return db.applyMigration3()
	case migrationVersion4:
		return db.applyMigration4()
	default:
		// For any new migrations, just apply them directly
		migrations := getMigrations()
		if migration, exists := migrations[version]; exists {
			return db.ApplyMigration(version, migration)
		}
		return fmt.Errorf("unknown migration version: %d", version)
	}
}

// applyMigration2 adds the latest_item_date column to feeds table.
func (db *DB) applyMigration2() error {
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
		return db.ApplyMigration(migrationVersion2, migrations[migrationVersion2])
	}

	// Column exists, just record the migration
	_, err = db.conn.Exec("INSERT INTO schema_migrations (version) VALUES (?)", migrationVersion2)
	if err != nil {
		return fmt.Errorf("failed to record migration %d: %w", migrationVersion2, err)
	}
	return nil
}

// applyMigration3 adds the url_metadata table.
func (db *DB) applyMigration3() error {
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
	return db.ApplyMigration(migrationVersion3, migrations[migrationVersion3])
}

// applyMigration4 adds the first_seen column to items table and backfills data.
func (db *DB) applyMigration4() error {
	// Check if first_seen column already exists
	var colCount int
	err := db.conn.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('items')
		WHERE name = 'first_seen'
	`).Scan(&colCount)
	if err != nil {
		return fmt.Errorf("failed to check column existence: %w", err)
	}

	if colCount == 0 {
		return db.applyMigration4WithBackfill()
	}

	// Column exists, just record the migration
	_, err = db.conn.Exec("INSERT INTO schema_migrations (version) VALUES (?)", migrationVersion4)
	if err != nil {
		return fmt.Errorf("failed to record migration %d: %w", migrationVersion4, err)
	}
	return nil
}

// applyMigration4WithBackfill adds the first_seen column and backfills existing data.
func (db *DB) applyMigration4WithBackfill() error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				logrus.WithError(rollbackErr).Warn("Failed to rollback transaction")
			}
		}
	}()

	// Add the column
	migrations := getMigrations()
	if _, err := tx.Exec(migrations[migrationVersion4]); err != nil {
		return fmt.Errorf("failed to add first_seen column: %w", err)
	}

	// Backfill first_seen for existing items
	logrus.Info("Backfilling first_seen timestamps for existing items...")
	backfillSQL := fmt.Sprintf(`
		UPDATE items SET first_seen =
			CASE
				WHEN published_date > datetime('now') THEN datetime('now')
				WHEN published_date < datetime('%s') THEN datetime('%s')
				ELSE published_date
			END
		WHERE first_seen IS NULL
	`, MinReasonableItemDate, MinReasonableItemDate)
	if _, err = tx.Exec(backfillSQL); err != nil {
		return fmt.Errorf("failed to backfill first_seen: %w", err)
	}

	// Update feeds.latest_item_date based on max first_seen
	logrus.Info("Updating feeds.latest_item_date based on item first_seen timestamps...")
	_, err = tx.Exec(`
		UPDATE feeds
		SET latest_item_date = (
			SELECT MAX(first_seen)
			FROM items
			WHERE items.feed_url = feeds.url
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to update feed latest_item_date: %w", err)
	}

	// Record the migration
	if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", migrationVersion4); err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migration: %w", err)
	}
	committed = true
	return nil
}
