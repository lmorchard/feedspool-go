package database

import (
	"os"
	"path/filepath"
	"testing"
)

func setupTestDB(t *testing.T) *DB {
	t.Helper()

	// Create temporary database file
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "feedspool_test.db")

	// Initialize database
	db, err := New(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := db.InitSchema(); err != nil {
		t.Fatal(err)
	}

	// Cleanup function
	t.Cleanup(func() {
		db.Close()
		os.Remove(dbPath)
	})

	return db
}
