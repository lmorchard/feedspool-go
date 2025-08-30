package database

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
	for version := currentVersion + 1; version <= len(migrations)+1; version++ {
		if migration, exists := migrations[version]; exists {
			if err := db.ApplyMigration(version, migration); err != nil {
				return err
			}
		}
	}

	return nil
}
