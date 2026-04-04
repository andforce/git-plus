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

func TestLatestMigrationBackfillsRepoRefsLastHashUpdatedAtFromUpdatedAt(t *testing.T) {
	dataDir := t.TempDir()
	sqlitePath := filepath.Join(dataDir, sqliteFilename)

	sqliteDB, err := openSQLite(sqlitePath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer sqliteDB.Close()

	if err := ensureMigrationsTable(context.Background(), sqliteDB); err != nil {
		t.Fatalf("ensure migrations table: %v", err)
	}

	migrationEntries, err := fs.ReadDir(embeddedMigrations, "migrations")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	if len(migrationEntries) < 2 {
		t.Fatalf("expected at least 2 migrations, got %d", len(migrationEntries))
	}

	latestMigration := migrationEntries[len(migrationEntries)-1].Name()
	for _, entry := range migrationEntries[:len(migrationEntries)-1] {
		if !entry.IsDir() {
			continue
		}
		if _, err := applyMigration(context.Background(), sqliteDB, entry.Name()); err != nil {
			t.Fatalf("apply pre-latest migration %s: %v", entry.Name(), err)
		}
	}

	_, err = sqliteDB.ExecContext(context.Background(), `
		INSERT INTO repos (
			id, source_id, platform, ref_id, status, name, full_name, owner,
			description, html_url, clone_url, ssh_url, default_branch, visibility,
			is_private, is_fork, is_archived, origin, meta, last_seen_at, disabled_at,
			created_at, updated_at
		) VALUES (
			1, 'source-a', 'github', '1', 'active', 'core', 'acme/core', 'acme',
			NULL, NULL, NULL, NULL, NULL, NULL,
			0, 0, 0, '{}', '{}', '2026-04-04T08:00:00Z', NULL,
			'2026-04-04T08:00:00Z', '2026-04-04T08:00:00Z'
		)
	`)
	if err != nil {
		t.Fatalf("insert repo: %v", err)
	}

	_, err = sqliteDB.ExecContext(context.Background(), `
		INSERT INTO repo_refs_current (
			id, repo_id, ref_name, ref_kind, current_hash, status, archive_ref_name,
			first_seen_at, last_seen_at, last_hash_updated_at, deleted_at, created_at, updated_at
		) VALUES (
			1, 1, 'refs/heads/main', 'head', 'abc123', 'active', 'refs/archive/heads/main/abc123',
			'2026-04-04T08:00:00Z', '2026-04-04T09:00:00Z', '2026-04-04T09:00:00Z', NULL,
			'2026-04-04T08:00:00Z', '2026-04-04T09:00:00Z'
		)
	`)
	if err != nil {
		t.Fatalf("insert repo ref current: %v", err)
	}

	if _, err := applyMigration(context.Background(), sqliteDB, latestMigration); err != nil {
		t.Fatalf("apply latest migration %s: %v", latestMigration, err)
	}

	var lastHashUpdatedAt string
	var updatedAt string
	if err := sqliteDB.QueryRowContext(context.Background(), `
		SELECT last_hash_updated_at, updated_at
		FROM repo_refs_current
		WHERE id = 1
	`).Scan(&lastHashUpdatedAt, &updatedAt); err != nil {
		t.Fatalf("query migrated repo ref current: %v", err)
	}

	if lastHashUpdatedAt != updatedAt {
		t.Fatalf("expected last_hash_updated_at to equal updated_at, got %q vs %q", lastHashUpdatedAt, updatedAt)
	}
}
