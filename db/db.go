package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

const sqliteFilename = "db.sqlite"

//go:embed migrations/*/migration.sql
var embeddedMigrations embed.FS

func Migrate(ctx context.Context, dataDir string) error {
	normalizedDataDir, err := normalizeDataDir(dataDir)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(normalizedDataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	sqliteDB, err := openSQLite(filepath.Join(normalizedDataDir, sqliteFilename))
	if err != nil {
		return err
	}
	defer sqliteDB.Close()

	if err := ensureMigrationsTable(ctx, sqliteDB); err != nil {
		return err
	}

	entries, err := fs.ReadDir(embeddedMigrations, "migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}

	migrationDirs := make([]string, 0, len(entries))

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		migrationDirs = append(migrationDirs, entry.Name())
	}

	if len(migrationDirs) == 0 {
		return nil
	}

	upToDate, err := migrationsUpToDate(ctx, sqliteDB, migrationDirs)
	if err != nil {
		return err
	}
	if upToDate {
		return nil
	}

	pendingCount := 0

	for _, migrationDir := range migrationDirs {
		applied, err := migrationApplied(ctx, sqliteDB, migrationDir)
		if err != nil {
			return err
		}
		if !applied {
			pendingCount++
		}
	}

	if pendingCount == 0 {
		return nil
	}

	log.Printf(
		"database: starting migrations for %s (count=%d)",
		filepath.Join(normalizedDataDir, sqliteFilename),
		pendingCount,
	)

	appliedCount := 0

	for _, migrationDir := range migrationDirs {
		applied, err := applyMigration(ctx, sqliteDB, migrationDir)
		if err != nil {
			return err
		}

		if applied {
			appliedCount++
		}
	}

	log.Printf("database: migrations complete, applied=%d", appliedCount)

	return nil
}

func Open(ctx context.Context, dataDir string) (*sql.DB, error) {
	normalizedDataDir, err := normalizeDataDir(dataDir)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(normalizedDataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	sqliteDB, err := openSQLite(filepath.Join(normalizedDataDir, sqliteFilename))
	if err != nil {
		return nil, err
	}

	if err := sqliteDB.PingContext(ctx); err != nil {
		sqliteDB.Close()
		return nil, fmt.Errorf("ping sqlite database: %w", err)
	}

	return sqliteDB, nil
}

func migrationsUpToDate(ctx context.Context, sqliteDB *sql.DB, migrationDirs []string) (bool, error) {
	var appliedCount int
	var latestApplied sql.NullString

	err := sqliteDB.QueryRowContext(
		ctx,
		"SELECT COUNT(1), COALESCE(MAX(filename), '') FROM schema_migrations",
	).Scan(&appliedCount, &latestApplied)
	if err != nil {
		return false, fmt.Errorf("query migration status: %w", err)
	}

	if appliedCount != len(migrationDirs) {
		return false, nil
	}

	return latestApplied.String == migrationDirs[len(migrationDirs)-1], nil
}

func normalizeDataDir(value string) (string, error) {
	normalizedValue := strings.TrimSpace(value)
	if normalizedValue == "" {
		return "", fmt.Errorf("data dir is required")
	}

	return normalizedValue, nil
}

func ensureMigrationsTable(ctx context.Context, sqliteDB *sql.DB) error {
	_, err := sqliteDB.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("ensure migrations table: %w", err)
	}

	return nil
}

func applyMigration(ctx context.Context, sqliteDB *sql.DB, migrationDir string) (bool, error) {
	applied, err := migrationApplied(ctx, sqliteDB, migrationDir)
	if err != nil {
		return false, err
	}
	if applied {
		return false, nil
	}

	log.Printf("database: applying migration %s", migrationDir)

	content, err := embeddedMigrations.ReadFile(filepath.ToSlash(filepath.Join("migrations", migrationDir, "migration.sql")))
	if err != nil {
		return false, fmt.Errorf("read migration %s: %w", migrationDir, err)
	}

	tx, err := sqliteDB.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin migration %s: %w", migrationDir, err)
	}

	if _, err := tx.ExecContext(ctx, string(content)); err != nil {
		_ = tx.Rollback()
		return false, fmt.Errorf("execute migration %s: %w", migrationDir, err)
	}

	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (filename) VALUES (?)", migrationDir); err != nil {
		_ = tx.Rollback()
		return false, fmt.Errorf("record migration %s: %w", migrationDir, err)
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit migration %s: %w", migrationDir, err)
	}

	log.Printf("database: applied migration %s", migrationDir)

	return true, nil
}

func migrationApplied(ctx context.Context, sqliteDB *sql.DB, filename string) (bool, error) {
	var count int

	err := sqliteDB.QueryRowContext(ctx, "SELECT COUNT(1) FROM schema_migrations WHERE filename = ?", filename).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("query migration %s: %w", filename, err)
	}

	return count > 0, nil
}

func openSQLite(path string) (*sql.DB, error) {
	sqliteDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	if _, err := sqliteDB.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		sqliteDB.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	return sqliteDB, nil
}
