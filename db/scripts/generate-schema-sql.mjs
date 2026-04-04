import { spawnSync } from 'node:child_process';
import { mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const dbDir = path.resolve(scriptDir, '..');
const schemaPath = path.join(dbDir, 'src', 'schema.ts');
const outputPath = path.join(dbDir, 'schema.sql');
const tempMigrationsDir = mkdtempSync(path.join(tmpdir(), 'git-plus-schema-sql-'));

try {
  const result = spawnSync(
    'pnpm',
    [
      'exec',
      'drizzle-kit',
      'generate',
      '--dialect',
      'sqlite',
      '--schema',
      schemaPath,
      '--out',
      tempMigrationsDir,
      '--name',
      'schema',
    ],
    {
      cwd: dbDir,
      stdio: 'inherit',
    },
  );

  if (result.status !== 0) {
    process.exit(result.status ?? 1);
  }

  const migrationDirs = readdirSync(tempMigrationsDir, {
    withFileTypes: true,
  })
    .filter((entry) => entry.isDirectory())
    .map((entry) => entry.name)
    .sort();

  if (migrationDirs.length !== 1) {
    throw new Error(
      `expected exactly 1 generated migration, got ${migrationDirs.length}`,
    );
  }

  const migrationPath = path.join(
    tempMigrationsDir,
    migrationDirs[0],
    'migration.sql',
  );
  const schemaSQL = readFileSync(migrationPath, 'utf8')
    .replaceAll('--> statement-breakpoint', '')
    .replaceAll('`', '')
    .trimEnd()
    .concat('\n');

  writeFileSync(outputPath, schemaSQL);
} finally {
  rmSync(tempMigrationsDir, { recursive: true, force: true });
}
