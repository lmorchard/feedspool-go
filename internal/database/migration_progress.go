package database

// MigrationProgress reports what a migration is doing so the command layer can
// show it. internal/database deliberately does not print: migrations log at
// info level and the default log level is Warn, which is how migration 11's
// twenty-odd seconds of indexing came to look like a hang.
//
// A nil MigrationProgress is silent, which is what every non-CLI caller wants.
type MigrationProgress interface {
	// Applying is called before a migration runs, so a migration that is about
	// to do bulk work -- or that fails partway through it -- has already said
	// what it was doing.
	Applying(version int, description string)
	// Progress reports work completed inside a single migration, counted
	// against what that migration found outstanding when it started.
	Progress(done, total int64)
}

// SetMigrationProgress installs the reporter used by subsequent migrations. It
// has to be set before the first call that migrates, which for most commands
// means between New and IsInitialized.
func (db *DB) SetMigrationProgress(progress MigrationProgress) {
	db.migrationProgress = progress
}

// announceMigration reports a migration that is about to run.
func (db *DB) announceMigration(version int) {
	if db.migrationProgress != nil {
		db.migrationProgress.Applying(version, migrationDescriptions()[version])
	}
}

// migrationProgressFunc adapts the reporter to the callback shape RunBackfill
// takes, and returns nil when there is nothing to report to.
func (db *DB) migrationProgressFunc() func(done, total int64) {
	if db.migrationProgress == nil {
		return nil
	}
	return db.migrationProgress.Progress
}

// migrationBackfillProgress reports a migration's bulk progress to both the log
// and the command layer. The log line stays the diagnostic record for anyone
// running with -v or reading a log file; the reporter is what a user sitting at
// a terminal actually sees.
func (db *DB) migrationBackfillProgress() func(done, total int64) {
	return db.migrationBackfillProgressWith(ItemTextProgressLogger())
}

// migrationBackfillProgressWith pairs a backfill-specific log line with the
// installed migration reporter, for backfills that are not about item text.
func (db *DB) migrationBackfillProgressWith(log func(done, total int64)) func(done, total int64) {
	reporter := db.migrationProgressFunc()
	return func(done, total int64) {
		log(done, total)
		if reporter != nil {
			reporter(done, total)
		}
	}
}
