package cmd

import (
	"fmt"
	"os"

	"github.com/lmorchard/feedspool-go/internal/database"
)

// migrationReporter shows migration progress while it happens.
//
// internal/database does not print. Migrations log at info level and the
// default log level is Warn, so migration 11's indexing pass -- tens of seconds
// on a large spool -- produced no output at all. What made that worse than a
// slow command is which command pays for it: IsInitialized runs pending
// migrations, so the process that blocks is whatever opens the database first
// after an upgrade, typically a cron fetch or an interactive status. The user
// did not ask to migrate.
//
// Output goes to stderr, not stdout. Any command can trigger a migration,
// including "items --format json", and progress lines on stdout would corrupt
// output something is piping into jq.
type migrationReporter struct{}

// Applying announces a migration before it runs, so a migration that is slow
// or fails partway through has already said what it was doing.
func (migrationReporter) Applying(version int, description string) {
	if description == "" {
		fmt.Fprintf(os.Stderr, "Migrating database to schema version %d...\n", version)
		return
	}
	fmt.Fprintf(os.Stderr, "Migrating database to schema version %d: %s...\n", version, description)
}

// Progress reports bulk work inside a single migration.
func (migrationReporter) Progress(done, total int64) {
	fmt.Fprintf(os.Stderr, "  %d of %d items\n", done, total)
}

// openDatabase connects to the spool, reports any migration it runs on the way
// in, and verifies the schema is usable. The caller owns the connection and is
// responsible for closing it.
func openDatabase(path string) (*database.DB, error) {
	db, err := database.New(path)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	// Installed before IsInitialized, which is the call that migrates.
	db.SetMigrationProgress(migrationReporter{})
	if err := db.IsInitialized(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
