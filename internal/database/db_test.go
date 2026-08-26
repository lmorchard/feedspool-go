package database

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewOpensExistingWALDatabaseDuringWrite(t *testing.T) {
	dbPath := initializedDatabasePath(t)
	writer := beginImmediate(t, dbPath)
	defer rollback(t, writer)

	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() during WAL write = %v", err)
	}
	defer db.Close()

	var count int
	if err := db.conn.QueryRow("SELECT COUNT(*) FROM feeds").Scan(&count); err != nil {
		t.Fatalf("concurrent read = %v", err)
	}
}

func TestWALRetryWaitsForLockRelease(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "delete-mode.db")
	seed, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seed.Exec("CREATE TABLE seed (id INTEGER PRIMARY KEY)"); err != nil {
		seed.Close()
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}

	writer := beginImmediate(t, dbPath)
	candidate, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer candidate.Close()
	if _, err := candidate.Exec("PRAGMA busy_timeout = 0"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	attempted := make(chan struct{})
	result := make(chan error, 1)
	attempts := 0
	var mode string
	go func() {
		result <- retrySQLiteBusy(ctx, func() error {
			attempts++
			if attempts == 1 {
				close(attempted)
			}
			return candidate.QueryRowContext(ctx, "PRAGMA journal_mode = WAL").Scan(&mode)
		})
	}()
	<-attempted

	select {
	case err := <-result:
		rollback(t, writer)
		t.Fatalf("WAL retry returned before lock release: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	rollback(t, writer)

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("WAL retry after lock release = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WAL retry did not finish after lock release")
	}
	if attempts < 2 {
		t.Fatalf("WAL attempts = %d, want a typed busy retry", attempts)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Fatalf("journal_mode = %q, want WAL", mode)
	}
}

func TestWALRetryHonorsDeadline(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "locked.db")
	seed, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seed.Exec("CREATE TABLE seed (id INTEGER PRIMARY KEY)"); err != nil {
		seed.Close()
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	writer := beginImmediate(t, dbPath)
	defer rollback(t, writer)

	candidate, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer candidate.Close()
	if _, err := candidate.Exec("PRAGMA busy_timeout = 0"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var mode string
	started := time.Now()
	err = retrySQLiteBusy(ctx, func() error {
		return candidate.QueryRowContext(ctx, "PRAGMA journal_mode = WAL").Scan(&mode)
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WAL retry error = %v, want context deadline", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("WAL retry exceeded hard deadline by too much: %v", elapsed)
	}
}

func TestConcurrentOpenAndRead(t *testing.T) {
	dbPath := initializedDatabasePath(t)
	const workers = 24
	start := make(chan struct{})
	errs := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			db, err := New(dbPath)
			if err != nil {
				errs <- err
				return
			}
			defer db.Close()
			var count int
			if err := db.conn.QueryRow("SELECT COUNT(*) FROM feeds").Scan(&count); err != nil {
				errs <- err
			}
		}()
	}
	close(start)
	group.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent open/read: %v", err)
	}
}

func TestIsInitializedReturnsMigrationFailure(t *testing.T) {
	db := setupTestDB(t)
	if _, err := db.conn.Exec("DROP TABLE items"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.conn.Exec("DELETE FROM schema_migrations WHERE version = ?", migrationVersion9); err != nil {
		t.Fatal(err)
	}

	err := db.IsInitialized()
	if err == nil {
		t.Fatal("IsInitialized() succeeded after a pending migration failed")
	}
	if !strings.Contains(err.Error(), "failed to run migrations") {
		t.Fatalf("IsInitialized() error = %q, want migration context", err)
	}
}

func initializedDatabasePath(t *testing.T) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "feeds.db")
	db, err := New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InitSchema(); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return dbPath
}

func beginImmediate(t *testing.T, dbPath string) *sql.Conn {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	return conn
}

func rollback(t *testing.T, conn *sql.Conn) {
	t.Helper()
	if conn == nil {
		return
	}
	if _, err := conn.ExecContext(context.Background(), "ROLLBACK"); err != nil {
		t.Errorf("rollback writer: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Errorf("close writer: %v", err)
	}
}
