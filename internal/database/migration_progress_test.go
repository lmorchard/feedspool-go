package database

import (
	"slices"
	"testing"

	"github.com/sirupsen/logrus"
	logrustest "github.com/sirupsen/logrus/hooks/test"
)

// recordingMigrationProgress captures what the migration runner reported.
type recordingMigrationProgress struct {
	applied  []int
	progress [][2]int64
}

func (r *recordingMigrationProgress) Applying(version int, _ string) {
	r.applied = append(r.applied, version)
}

func (r *recordingMigrationProgress) Progress(done, total int64) {
	r.progress = append(r.progress, [2]int64{done, total})
}

// A migration that does bulk work has to say so before it starts. IsInitialized
// runs migrations, so the command that pays for migration 11 is usually an
// incidental status or a cron fetch -- something the user did not ask to
// migrate, which then goes quiet for tens of seconds on a large spool.
func TestRunMigrationsAnnouncesAMigrationBeforeRunningIt(t *testing.T) {
	const seedCount = 3
	db := seedSearchableItems(t, seedCount)
	rewindPastMigration11(t, db)

	recorder := &recordingMigrationProgress{}
	db.SetMigrationProgress(recorder)
	if err := db.RunMigrations(); err != nil {
		t.Fatal(err)
	}

	if !slices.Contains(recorder.applied, migrationVersion11) {
		t.Errorf("migration %d ran without announcing itself; announced %v",
			migrationVersion11, recorder.applied)
	}
	if len(recorder.progress) == 0 {
		t.Error("migration 11 indexed every item without reporting any progress")
	}
	for _, sample := range recorder.progress {
		if sample[1] != seedCount {
			t.Errorf("progress reported %d items outstanding, want %d", sample[1], seedCount)
		}
	}
}

// A nil reporter is the case every non-CLI caller takes -- the renderer, and
// every test that does not care -- so it must not panic.
func TestRunMigrationsToleratesNoProgressReporter(t *testing.T) {
	db := seedSearchableItems(t, 1)
	rewindPastMigration11(t, db)

	if err := db.RunMigrations(); err != nil {
		t.Fatal(err)
	}
	if got := countItemTextRows(t, db); got != 1 {
		t.Errorf("migration indexed %d rows without a reporter, want 1", got)
	}
}

// ApplyMigration's deferred rollback fires after a successful commit and logs
// the resulting sql.ErrTxDone as a warning. Its own comment says it means to
// skip that case. Four of these are the entire visible output of a real
// upgrade at the default log level, which is alarming in exactly the moment a
// user is paying closest attention.
func TestApplyMigrationDoesNotWarnAfterASuccessfulCommit(t *testing.T) {
	db := setupTestDB(t)

	hook := logrustest.NewLocal(logrus.StandardLogger())
	defer hook.Reset()

	if err := db.ApplyMigration(maxMigrationVersion+1, `CREATE TABLE IF NOT EXISTS probe (x INTEGER)`); err != nil {
		t.Fatal(err)
	}

	for _, entry := range hook.AllEntries() {
		if entry.Level <= logrus.WarnLevel {
			t.Errorf("a successful migration logged %s: %q", entry.Level, entry.Message)
		}
	}
}
