package db

import (
	"context"
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateCreatesSQLiteFileAndIsIdempotent(t *testing.T) {
	dataDir := t.TempDir()

	if err := Migrate(context.Background(), dataDir); err != nil {
		t.Fatalf("first migrate failed: %v", err)
	}

	sqlitePath := filepath.Join(dataDir, sqliteFilename)
	if _, err := os.Stat(sqlitePath); err != nil {
		t.Fatalf("expected sqlite database at %s: %v", sqlitePath, err)
	}

	sqliteDB, err := sql.Open("sqlite", sqlitePath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer sqliteDB.Close()

	if err := Migrate(context.Background(), dataDir); err != nil {
		t.Fatalf("second migrate failed: %v", err)
	}

	var appMetaCount int
	if err := sqliteDB.QueryRow("SELECT COUNT(1) FROM sqlite_master WHERE type = 'table' AND name = 'app_meta'").Scan(&appMetaCount); err != nil {
		t.Fatalf("query app_meta table: %v", err)
	}
	if appMetaCount != 1 {
		t.Fatalf("expected app_meta table to exist once, got %d", appMetaCount)
	}

	var repoCount int
	if err := sqliteDB.QueryRow("SELECT COUNT(1) FROM sqlite_master WHERE type = 'table' AND name = 'repos'").Scan(&repoCount); err != nil {
		t.Fatalf("query repos table: %v", err)
	}
	if repoCount != 1 {
		t.Fatalf("expected repos table to exist once, got %d", repoCount)
	}

	var migrationCount int
	if err := sqliteDB.QueryRow("SELECT COUNT(1) FROM schema_migrations").Scan(&migrationCount); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}

	migrationEntries, err := fs.ReadDir(embeddedMigrations, "migrations")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}

	expectedMigrationCount := 0
	for _, entry := range migrationEntries {
		if entry.IsDir() {
			expectedMigrationCount++
		}
	}

	if migrationCount != expectedMigrationCount {
		t.Fatalf("expected %d applied migrations, got %d", expectedMigrationCount, migrationCount)
	}
}

func TestOpenConfiguresSQLitePragmas(t *testing.T) {
	dataDir := t.TempDir()

	if err := Migrate(context.Background(), dataDir); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	sqliteDB, err := Open(context.Background(), dataDir)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer sqliteDB.Close()

	var foreignKeys int
	if err := sqliteDB.QueryRow("PRAGMA foreign_keys;").Scan(&foreignKeys); err != nil {
		t.Fatalf("query foreign_keys pragma: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("expected foreign_keys=1, got %d", foreignKeys)
	}

	var journalMode string
	if err := sqliteDB.QueryRow("PRAGMA journal_mode;").Scan(&journalMode); err != nil {
		t.Fatalf("query journal_mode pragma: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("expected journal_mode=wal, got %q", journalMode)
	}

	var busyTimeout int
	if err := sqliteDB.QueryRow("PRAGMA busy_timeout;").Scan(&busyTimeout); err != nil {
		t.Fatalf("query busy_timeout pragma: %v", err)
	}
	if busyTimeout != sqliteBusyTimeoutMillis {
		t.Fatalf("expected busy_timeout=%d, got %d", sqliteBusyTimeoutMillis, busyTimeout)
	}
}
