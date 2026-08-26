package database

import (
	"fmt"

	"github.com/sirupsen/logrus"
)

const (
	// Migration version constants.
	migrationVersion1   = 1  // Initial schema (handled by InitSchema)
	migrationVersion2   = 2  // Add latest_item_date column to feeds
	migrationVersion3   = 3  // Add url_metadata table
	migrationVersion4   = 4  // Add first_seen column to items
	migrationVersion5   = 5  // Add user_agent column to feeds
	migrationVersion6   = 6  // Add item_annotations table
	migrationVersion7   = 7  // Add discovery-time query index
	migrationVersion8   = 8  // Add feed parser type and scrape selector
	migrationVersion9   = 9  // Add effective-date query index
	migrationVersion10  = 10 // Deduplicate annotations and enforce uniqueness
	maxMigrationVersion = migrationVersion10
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
		migrationVersion5: `ALTER TABLE feeds ADD COLUMN user_agent TEXT NOT NULL DEFAULT '';`,
		migrationVersion6: `CREATE TABLE IF NOT EXISTS item_annotations (
			feed_url   TEXT NOT NULL,
			item_guid  TEXT NOT NULL,
			kind       TEXT NOT NULL,
			value      TEXT,
			actor      TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (feed_url, item_guid) REFERENCES items(feed_url, guid) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_item_annotations_lookup ON item_annotations(feed_url, item_guid, kind);
		CREATE INDEX IF NOT EXISTS idx_item_annotations_kind   ON item_annotations(kind, created_at);`,
		migrationVersion7: fmt.Sprintf(
			"CREATE INDEX IF NOT EXISTS idx_items_discovery_time ON items(%s);",
			discoveryTimeExpression,
		),
		migrationVersion8: `ALTER TABLE feeds ADD COLUMN type TEXT NOT NULL DEFAULT 'rss';
		ALTER TABLE feeds ADD COLUMN scrape_selector TEXT NOT NULL DEFAULT '';`,
		migrationVersion9: fmt.Sprintf(
			`CREATE INDEX IF NOT EXISTS idx_items_effective_date ON items(%[1]s);
			CREATE INDEX IF NOT EXISTS idx_items_feed_effective_date ON items(feed_url, %[1]s);`,
			effectiveDateExpression,
		),
		// COALESCE(value, '') is load-bearing: SQLite treats NULLs as distinct
		// in a unique index, so ('feed', 'guid', 'seen', NULL) would never
		// conflict with itself and duplicate "seen" rows would still be possible.
		migrationVersion10: `DELETE FROM item_annotations
		WHERE rowid NOT IN (
			SELECT rowid FROM (
				SELECT rowid,
					ROW_NUMBER() OVER (
						PARTITION BY feed_url, item_guid, kind, COALESCE(value, '')
						ORDER BY created_at ASC, rowid ASC
					) AS row_rank
				FROM item_annotations
			)
			WHERE row_rank = 1
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_item_annotations_unique
			ON item_annotations(feed_url, item_guid, kind, COALESCE(value, ''));`,
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
			logrus.Infof("Applying migration %d", version)
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
	case migrationVersion5:
		return db.applyMigration5()
	case migrationVersion6:
		return db.applyMigration6()
	case migrationVersion8:
		return db.applyMigration8()
	case migrationVersion9:
		return db.applyMigration9()
	case migrationVersion10:
		return db.applyMigration10()
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

// applyMigration5 adds the user_agent column to feeds table.
func (db *DB) applyMigration5() error {
	// Check if user_agent column already exists
	var colCount int
	err := db.conn.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('feeds')
		WHERE name = 'user_agent'
	`).Scan(&colCount)
	if err != nil {
		return fmt.Errorf("failed to check column existence: %w", err)
	}

	if colCount == 0 {
		migrations := getMigrations()
		return db.ApplyMigration(migrationVersion5, migrations[migrationVersion5])
	}

	// Column exists, just record the migration
	_, err = db.conn.Exec("INSERT INTO schema_migrations (version) VALUES (?)", migrationVersion5)
	if err != nil {
		return fmt.Errorf("failed to record migration %d: %w", migrationVersion5, err)
	}
	return nil
}

// applyMigration6 adds the item_annotations table.
func (db *DB) applyMigration6() error {
	// Check if item_annotations table already exists
	var tableCount int
	err := db.conn.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type='table' AND name='item_annotations'
	`).Scan(&tableCount)
	if err != nil {
		return fmt.Errorf("failed to check table existence: %w", err)
	}

	migrations := getMigrations()
	return db.ApplyMigration(migrationVersion6, migrations[migrationVersion6])
}

// applyMigration8 adds feed parser configuration columns.
func (db *DB) applyMigration8() error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin migration %d: %w", migrationVersion8, err)
	}
	committed := false
	defer func() {
		if !committed {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				logrus.WithError(rollbackErr).Warn("Failed to rollback migration")
			}
		}
	}()

	columns := []struct {
		name string
		sql  string
	}{
		{name: "type", sql: `ALTER TABLE feeds ADD COLUMN type TEXT NOT NULL DEFAULT 'rss'`},
		{name: "scrape_selector", sql: `ALTER TABLE feeds ADD COLUMN scrape_selector TEXT NOT NULL DEFAULT ''`},
	}
	for _, column := range columns {
		var count int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('feeds') WHERE name = ?`, column.name).
			Scan(&count); err != nil {
			return fmt.Errorf("failed to check %s column: %w", column.name, err)
		}
		if count == 0 {
			if _, err := tx.Exec(column.sql); err != nil {
				return fmt.Errorf("failed to add %s column: %w", column.name, err)
			}
		}
	}

	if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", migrationVersion8); err != nil {
		return fmt.Errorf("failed to record migration %d: %w", migrationVersion8, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migration %d: %w", migrationVersion8, err)
	}
	committed = true
	return nil
}

// applyMigration9 normalizes legacy item timestamps before indexing chronological queries.
func (db *DB) applyMigration9() error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin migration %d: %w", migrationVersion9, err)
	}
	committed := false
	defer func() {
		if !committed {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				logrus.WithError(rollbackErr).Warn("Failed to rollback migration")
			}
		}
	}()

	type itemTimestamps struct {
		rowID         int64
		publishedDate interface{}
		firstSeen     interface{}
	}
	rows, err := tx.Query(`SELECT rowid, published_date, first_seen FROM items`)
	if err != nil {
		return fmt.Errorf("failed to read item timestamps: %w", err)
	}
	defer rows.Close()
	var records []itemTimestamps
	for rows.Next() {
		var record itemTimestamps
		if err := rows.Scan(&record.rowID, &record.publishedDate, &record.firstSeen); err != nil {
			return fmt.Errorf("failed to scan item timestamps: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to iterate item timestamps: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("failed to close item timestamp rows: %w", err)
	}

	for _, record := range records {
		publishedDate := normalizedMigrationTime(record.publishedDate)
		firstSeen := normalizedMigrationTime(record.firstSeen)
		if _, err := tx.Exec(
			`UPDATE items SET published_date = ?, first_seen = ? WHERE rowid = ?`,
			publishedDate, firstSeen, record.rowID,
		); err != nil {
			return fmt.Errorf("failed to normalize item timestamps: %w", err)
		}
	}

	migration := getMigrations()[migrationVersion9]
	if _, err := tx.Exec(migration); err != nil {
		return fmt.Errorf("failed to create effective-date indexes: %w", err)
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", migrationVersion9); err != nil {
		return fmt.Errorf("failed to record migration %d: %w", migrationVersion9, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migration %d: %w", migrationVersion9, err)
	}
	committed = true
	return nil
}

func normalizedMigrationTime(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	timestamp, err := parseDatabaseTime(value)
	if err != nil {
		return value
	}
	return formatDatabaseTime(timestamp)
}

// applyMigration10 removes duplicate annotations and adds the uniqueness index
// that keeps them from coming back.
//
// AddAnnotation was a bare INSERT against a table with no unique constraint, so
// running "feedspool mark-seen <link>" twice wrote two rows. The dedupe keeps
// the earliest row per group so a "first seen" reading stays honest, breaking
// ties on rowid because CURRENT_TIMESTAMP only has one-second resolution.
func (db *DB) applyMigration10() error {
	migrations := getMigrations()
	return db.ApplyMigration(migrationVersion10, migrations[migrationVersion10])
}
