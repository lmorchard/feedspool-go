package database

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lmorchard/feedspool-go/internal/config"
	_ "github.com/mattn/go-sqlite3" // sqlite3 driver
	"github.com/sirupsen/logrus"
)

//go:embed schema.sql
var schemaSQL string

// DB wraps a database connection with methods for feed operations.
type DB struct {
	conn *sql.DB
}

// New creates a new database connection and initializes it.
func New(dbPath string) (*DB, error) {
	dir := filepath.Dir(dbPath)
	if dir != "." && dir != "/" {
		if err := os.MkdirAll(dir, config.DefaultDirPerm); err != nil {
			return nil, fmt.Errorf("failed to create database directory: %w", err)
		}
	}

	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	if _, err := conn.Exec("PRAGMA journal_mode = WAL"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to set journal mode: %w", err)
	}

	if _, err := conn.Exec("PRAGMA synchronous = NORMAL"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to set synchronous mode: %w", err)
	}

	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)

	logrus.Debug("Database connection established")
	return &DB{conn: conn}, nil
}

// InitSchema initializes the database schema.
func (db *DB) InitSchema() error {
	_, err := db.conn.Exec(schemaSQL)
	if err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Run any pending migrations
	if err := db.RunMigrations(); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	logrus.Debug("Database schema initialized")
	return nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	if db.conn != nil {
		return db.conn.Close()
	}
	return nil
}

// IsInitialized checks if the database is properly initialized with required tables.
func (db *DB) IsInitialized() error {
	// Check if the feeds table exists by trying to query it
	var count int
	err := db.conn.QueryRow("SELECT COUNT(*) FROM feeds LIMIT 1").Scan(&count)
	if err != nil {
		return fmt.Errorf("database not initialized - run 'feedspool init' first")
	}

	// Run any pending migrations for existing databases
	if err := db.RunMigrations(); err != nil {
		logrus.Warnf("Failed to run migrations: %v", err)
		// Don't fail here - the database is still usable even if migrations fail
	}

	return nil
}

// GetMigrationVersion returns the current migration version.
func (db *DB) GetMigrationVersion() (int, error) {
	var version sql.NullInt64
	err := db.conn.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version)
	if err != nil {
		// Migrations table doesn't exist - this is an old database that needs migrations
		// This is not an error condition, so we return 0 version
		return 0, nil //nolint:nilerr
	}

	if !version.Valid {
		return 0, nil
	}
	return int(version.Int64), nil
}

// GetConnection returns the underlying database connection for testing purposes.
func (db *DB) GetConnection() *sql.DB {
	return db.conn
}

// ApplyMigration applies a database migration.
func (db *DB) ApplyMigration(version int, migrationSQL string) error {
	tx, err := db.conn.Begin()
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

	return nil
}

// Vacuum runs VACUUM on the database to reclaim space and optimize the database file.
func (db *DB) Vacuum() error {
	logrus.Debug("Running VACUUM on database")
	_, err := db.conn.Exec("VACUUM")
	if err != nil {
		return fmt.Errorf("failed to vacuum database: %w", err)
	}
	logrus.Debug("VACUUM completed successfully")
	return nil
}
