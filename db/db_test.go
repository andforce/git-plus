package db

import (
	"context"
	"database/sql"
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

	var migrationCount int
	if err := sqliteDB.QueryRow("SELECT COUNT(1) FROM schema_migrations").Scan(&migrationCount); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("expected one applied migration, got %d", migrationCount)
	}
}
