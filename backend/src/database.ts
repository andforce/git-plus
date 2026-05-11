import { existsSync, mkdirSync, readFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import Database from 'better-sqlite3';
import type { AppDatabase } from './types';

const SQLITE_FILENAME = 'db.sqlite';

export function openDatabase(dataDir: string): AppDatabase {
  mkdirSync(dataDir, { recursive: true });
  const db = new Database(join(dataDir, SQLITE_FILENAME));
  db.pragma('busy_timeout = 5000');
  db.pragma('journal_mode = WAL');
  db.pragma('foreign_keys = ON');
  migrateDatabase(db);
  return db;
}

function migrateDatabase(db: AppDatabase): void {
  db.exec(`
    CREATE TABLE IF NOT EXISTS schema_migrations (
      filename TEXT PRIMARY KEY,
      applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
    );
  `);
  const schema = readSchemaSql()
    .replaceAll('CREATE TABLE ', 'CREATE TABLE IF NOT EXISTS ')
    .replaceAll('CREATE UNIQUE INDEX ', 'CREATE UNIQUE INDEX IF NOT EXISTS ')
    .replaceAll('CREATE INDEX ', 'CREATE INDEX IF NOT EXISTS ');
  db.exec(schema);
}

function readSchemaSql(): string {
  const bundledDir = typeof __dirname === 'string' ? __dirname : '';
  const candidates = [
    resolve(process.cwd(), 'db/schema.sql'),
    resolve(process.cwd(), '../db/schema.sql'),
    ...(bundledDir ? [join(bundledDir, 'schema.sql')] : []),
  ];
  for (const candidate of candidates) {
    if (existsSync(candidate)) return readFileSync(candidate, 'utf8');
  }
  throw new Error('db/schema.sql was not found');
}
