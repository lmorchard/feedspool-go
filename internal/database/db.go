package database

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3" // sqlite3 driver
	"github.com/sirupsen/logrus"
)

//go:embed schema.sql
var schemaSQL string

var db *sql.DB

func Connect(dbPath string) error {
	dir := filepath.Dir(dbPath)
	if dir != "." && dir != "/" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create database directory: %w", err)
		}
	}

	var err error
	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		return fmt.Errorf("failed to set journal mode: %w", err)
	}

	if _, err := db.Exec("PRAGMA synchronous = NORMAL"); err != nil {
		return fmt.Errorf("failed to set synchronous mode: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	logrus.Debug("Database connection established")
	return nil
}

func InitSchema() error {
	if db == nil {
		return fmt.Errorf("database not connected")
	}

	_, err := db.Exec(schemaSQL)
	if err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	logrus.Debug("Database schema initialized")
	return nil
}

func GetDB() *sql.DB {
	return db
}

func Close() error {
	if db != nil {
		return db.Close()
	}
	return nil
}

// IsInitialized checks if the database is properly initialized with required tables.
func IsInitialized() error {
	if db == nil {
		return fmt.Errorf("database not connected")
	}

	// Check if the feeds table exists by trying to query it
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM feeds LIMIT 1").Scan(&count)
	if err != nil {
		return fmt.Errorf("database not initialized - run 'feedspool init' first")
	}

	return nil
}

func GetMigrationVersion() (int, error) {
	if db == nil {
		return 0, fmt.Errorf("database not connected")
	}

	var version int
	err := db.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version)
	if err != nil {
		return 0, err
	}

	return version, nil
}

func ApplyMigration(version int, migrationSQL string) error {
	if db == nil {
		return fmt.Errorf("database not connected")
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			// Only log rollback errors if they're not transaction already committed
			logrus.WithError(rollbackErr).Warn("Failed to rollback transaction")
		}
	}()

	if _, err := tx.Exec(migrationSQL); err != nil {
		return fmt.Errorf("failed to apply migration %d: %w", version, err)
	}

	if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", version); err != nil {
		return fmt.Errorf("failed to record migration %d: %w", version, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migration %d: %w", version, err)
	}

	logrus.Infof("Applied migration version %d", version)
	return nil
}
