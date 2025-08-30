package database

import (
	"github.com/sirupsen/logrus"
)

// getMigrations returns the database migration scripts.
func getMigrations() map[int]string {
	return map[int]string{
		2: `ALTER TABLE feeds ADD COLUMN latest_item_date DATETIME;`,
	}
}

// RunMigrations applies any pending database migrations.
func (db *DB) RunMigrations() error {
	currentVersion, err := db.GetMigrationVersion()
	if err != nil {
		// If the migrations table doesn't exist, assume version 0
		currentVersion = 0
	}

	migrations := getMigrations()
	maxVersion := len(migrations) + 1

	// Check if any migrations are needed
	if currentVersion >= maxVersion-1 {
		return nil // No migrations needed
	}

	logrus.Infof("Checking for database migrations (current version: %d)", currentVersion)

	appliedCount := 0
	for version := currentVersion + 1; version <= maxVersion; version++ {
		if migration, exists := migrations[version]; exists {
			logrus.Infof("Applying migration %d: Adding latest_item_date column", version)
			if err := db.ApplyMigration(version, migration); err != nil {
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
