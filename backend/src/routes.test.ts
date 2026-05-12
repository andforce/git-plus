import { execFileSync } from 'node:child_process';
import {
  mkdirSync,
  mkdtempSync,
  renameSync,
  rmSync,
  writeFileSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { createClient } from '@connectrpc/connect';
import { createConnectTransport } from '@connectrpc/connect-node';
import { fastifyConnectPlugin } from '@connectrpc/connect-fastify';
import { fastify } from 'fastify';
import { afterEach, describe, expect, it } from 'vitest';
import { RepoService } from '../../frontend/src/rpc/gitplus/repo/v1/repo_pb';
import { openDatabase } from './database';
import { DownloadManager } from './download';
import { createRoutes, handleRawBlobRequest } from './routes';
import type { AddressInfo } from 'node:net';
import type { RuntimeDeps } from './routes';

const tempDirs: Array<string> = [];

afterEach(() => {
  for (const dir of tempDirs.splice(0)) {
    rmSync(dir, { recursive: true, force: true });
  }
});

describe('repo routes', () => {
  it('reads tree, blob, README, and commits from the archived repository', async () => {
    const dataDir = tempDir();
    const db = openDatabase(dataDir);
    const { mainHash } = createArchiveFixture(dataDir, 'source-a', '1');

    db.prepare(
      `
      INSERT INTO repos (
        id, source_id, platform, ref_id, status, name, full_name, owner,
        description, html_url, clone_url, ssh_url, default_branch, visibility,
        is_private, is_fork, is_archived, origin, meta, last_seen_at,
        disabled_at, created_at, updated_at
      ) VALUES (
        1, 'source-a', 'github', '1', 'active', 'core', 'acme/core', 'acme',
        NULL, NULL, NULL, NULL, 'main', NULL,
        0, 0, 0, '{}', '{}', '2026-04-04T10:00:00Z',
        NULL, '2026-04-04T08:00:00Z', '2026-04-04T10:00:00Z'
      )
    `,
    ).run();
    db.prepare(
      `
      INSERT INTO repo_refs_current (
        repo_id, ref_name, ref_kind, current_hash, status, archive_ref_name,
        first_seen_at, last_seen_at, last_hash_updated_at, deleted_at,
        created_at, updated_at
      ) VALUES (
        1, 'refs/heads/main', 'head', ?, 'active', NULL,
        '2026-04-04T08:00:00Z', '2026-04-04T10:00:00Z',
        '2026-04-04T10:00:00Z', NULL,
        '2026-04-04T08:00:00Z', '2026-04-04T10:00:00Z'
      )
    `,
    ).run(mainHash);

    const server = fastify();
    const deps = {
      dataDir,
      db,
      bus: {},
      tasks: {},
      downloads: new DownloadManager(),
      cron: {},
    } as RuntimeDeps;
    await server.register(fastifyConnectPlugin, {
      prefix: '/api',
      routes: createRoutes(deps),
    });
    server.get('/api/repos/:repoId/raw', (request, reply) =>
      handleRawBlobRequest(deps, request, reply),
    );
    await server.listen({ port: 0, host: '127.0.0.1' });
    try {
      const address = server.server.address() as AddressInfo;
      const client = createClient(
        RepoService,
        createConnectTransport({
          baseUrl: `http://127.0.0.1:${address.port}/api`,
          httpVersion: '1.1',
        }),
      );

      const tree = await client.listTree({ repoId: 1n });
      expect(tree.refName).toBe('refs/heads/main');
      expect(tree.commitHash).toBe(mainHash);
      expect(tree.entries.map((entry) => entry.path)).toEqual(['README.md']);
      expect(tree.readme?.content).toBe('# Hello\n');

      const search = await client.searchFiles({
        repoId: 1n,
        refName: 'main',
        query: 'read',
        pageSize: 10,
      });
      expect(search.entries.map((entry) => entry.path)).toEqual(['README.md']);

      const codeSearch = await client.searchCode({
        repoId: 1n,
        refName: 'main',
        query: 'Hello',
        pageSize: 10,
      });
      expect(codeSearch.matches).toHaveLength(1);
      expect(codeSearch.matches[0]?.path).toBe('README.md');
      expect(codeSearch.matches[0]?.lineNo).toBe(1);
      expect(codeSearch.matches[0]?.line).toBe('# Hello');

      const blob = await client.getBlob({
        repoId: 1n,
        refName: 'main',
        path: 'README.md',
      });
      expect(blob.content).toBe('# Hello\n');
      expect(blob.isBinary).toBe(false);

      const blobAtCommit = await client.getBlob({
        repoId: 1n,
        refName: mainHash.slice(0, 12),
        path: 'README.md',
      });
      expect(blobAtCommit.commitHash).toBe(mainHash);
      expect(blobAtCommit.content).toBe('# Hello\n');

      const blame = await client.getBlame({
        repoId: 1n,
        refName: 'main',
        path: 'README.md',
      });
      expect(blame.lines).toHaveLength(1);
      expect(blame.lines[0]?.commitHash).toBe(mainHash);
      expect(blame.lines[0]?.authorName).toBe('Test');
      expect(blame.lines[0]?.content).toBe('# Hello');

      const commits = await client.listCommits({
        repoId: 1n,
        refName: 'refs/heads/main',
        pageSize: 10,
      });
      expect(commits.commits).toHaveLength(1);
      expect(commits.commits[0]?.hash).toBe(mainHash);
      expect(commits.commits[0]?.message).toContain('init');

      const readmeCommits = await client.listCommits({
        repoId: 1n,
        refName: 'main',
        pageSize: 10,
        path: 'README.md',
      });
      expect(readmeCommits.commits).toHaveLength(1);
      expect(readmeCommits.commits[0]?.hash).toBe(mainHash);

      const missingPathCommits = await client.listCommits({
        repoId: 1n,
        refName: 'main',
        pageSize: 10,
        path: 'src/missing.ts',
      });
      expect(missingPathCommits.commits).toHaveLength(0);

      const commit = await client.getCommit({
        repoId: 1n,
        commitHash: mainHash,
      });
      expect(commit.commit?.hash).toBe(mainHash);
      expect(commit.additions).toBeGreaterThan(0);
      expect(commit.files).toHaveLength(1);
      expect(commit.files[0]?.newPath).toBe('README.md');
      expect(commit.files[0]?.status).toBe('added');
      expect(commit.files[0]?.patch).toContain('+# Hello');

      const rawUrl = new URL(
        `http://127.0.0.1:${address.port}/api/repos/1/raw`,
      );
      rawUrl.searchParams.set('ref', 'main');
      rawUrl.searchParams.set('path', 'README.md');
      const raw = await fetch(rawUrl);
      expect(raw.status).toBe(200);
      expect(raw.headers.get('content-type')).toContain('text/markdown');
      expect(await raw.text()).toBe('# Hello\n');
    } finally {
      await server.close();
      db.close();
    }
  });

  it('compares branches from the archived repository', async () => {
    const dataDir = tempDir();
    const db = openDatabase(dataDir);
    const { mainHash, featureHash } = createCompareArchiveFixture(
      dataDir,
      'source-a',
      '1',
    );

    seedRepoRow(db);
    seedRouteRef(db, 'refs/heads/main', 'head', mainHash);
    seedRouteRef(db, 'refs/heads/feature', 'head', featureHash);

    const server = fastify();
    const deps = {
      dataDir,
      db,
      bus: {},
      tasks: {},
      downloads: new DownloadManager(),
      cron: {},
    } as RuntimeDeps;
    await server.register(fastifyConnectPlugin, {
      prefix: '/api',
      routes: createRoutes(deps),
    });
    await server.listen({ port: 0, host: '127.0.0.1' });
    try {
      const address = server.server.address() as AddressInfo;
      const client = createClient(
        RepoService,
        createConnectTransport({
          baseUrl: `http://127.0.0.1:${address.port}/api`,
          httpVersion: '1.1',
        }),
      );

      const comparison = await client.compareRefs({
        repoId: 1n,
        baseRefName: 'main',
        headRefName: 'feature',
        pageSize: 10,
      });

      expect(comparison.baseRefName).toBe('refs/heads/main');
      expect(comparison.headRefName).toBe('refs/heads/feature');
      expect(comparison.baseCommitHash).toBe(mainHash);
      expect(comparison.headCommitHash).toBe(featureHash);
      expect(comparison.mergeBaseHash).toBe(mainHash);
      expect(comparison.aheadCount).toBe(1);
      expect(comparison.behindCount).toBe(0);
      expect(comparison.commits.map((commit) => commit.hash)).toEqual([
        featureHash,
      ]);
      expect(comparison.additions).toBeGreaterThan(0);
      expect(comparison.files.map((file) => file.newPath).sort()).toEqual([
        'README.md',
        'src/app.ts',
      ]);
      expect(
        comparison.files.find((file) => file.newPath === 'src/app.ts')?.status,
      ).toBe('added');
    } finally {
      await server.close();
      db.close();
    }
  });
});

function tempDir(): string {
  const dir = mkdtempSync(join(tmpdir(), 'git-plus-routes-'));
  tempDirs.push(dir);
  return dir;
}

function createArchiveFixture(
  dataDir: string,
  sourceId: string,
  refId: string,
): { mainHash: string } {
  const workPath = join(tempDir(), 'source');
  git(dirname(workPath), 'init', '--initial-branch=main', workPath);
  git(workPath, 'config', 'user.name', 'Test');
  git(workPath, 'config', 'user.email', 'test@example.com');
  writeFileSync(join(workPath, 'README.md'), '# Hello\n');
  git(workPath, 'add', 'README.md');
  git(workPath, 'commit', '-m', 'init');

  const mainHash = gitOutput(workPath, 'rev-parse', 'main');
  const barePath = join(tempDir(), 'archive.git');
  git(dirname(barePath), 'clone', '--bare', workPath, barePath);
  git(
    dirname(barePath),
    '--git-dir',
    barePath,
    'update-ref',
    `refs/archive/heads/main/${mainHash}`,
    mainHash,
  );
  git(
    dirname(barePath),
    '--git-dir',
    barePath,
    'update-ref',
    '-d',
    'refs/heads/main',
  );
  git(
    dirname(barePath),
    '--git-dir',
    barePath,
    'symbolic-ref',
    'HEAD',
    'refs/heads/missing',
  );

  const targetPath = join(dataDir, 'repos', sourceId, refId);
  mkdirSync(dirname(targetPath), { recursive: true });
  renameSync(barePath, targetPath);
  return { mainHash };
}

function createCompareArchiveFixture(
  dataDir: string,
  sourceId: string,
  refId: string,
): { mainHash: string; featureHash: string } {
  const workPath = join(tempDir(), 'source');
  git(dirname(workPath), 'init', '--initial-branch=main', workPath);
  git(workPath, 'config', 'user.name', 'Test');
  git(workPath, 'config', 'user.email', 'test@example.com');
  writeFileSync(join(workPath, 'README.md'), '# Hello\n');
  git(workPath, 'add', 'README.md');
  git(workPath, 'commit', '-m', 'init');
  const mainHash = gitOutput(workPath, 'rev-parse', 'main');

  git(workPath, 'switch', '-c', 'feature');
  mkdirSync(join(workPath, 'src'), { recursive: true });
  writeFileSync(join(workPath, 'README.md'), '# Hello\n\nFeature\n');
  writeFileSync(join(workPath, 'src', 'app.ts'), 'export const value = 1;\n');
  git(workPath, 'add', 'README.md', 'src/app.ts');
  git(workPath, 'commit', '-m', 'add feature');
  const featureHash = gitOutput(workPath, 'rev-parse', 'feature');

  const barePath = join(tempDir(), 'archive.git');
  git(dirname(barePath), 'clone', '--bare', workPath, barePath);
  git(
    dirname(barePath),
    '--git-dir',
    barePath,
    'update-ref',
    `refs/archive/heads/main/${mainHash}`,
    mainHash,
  );
  git(
    dirname(barePath),
    '--git-dir',
    barePath,
    'update-ref',
    `refs/archive/heads/feature/${featureHash}`,
    featureHash,
  );
  git(
    dirname(barePath),
    '--git-dir',
    barePath,
    'update-ref',
    '-d',
    'refs/heads/main',
  );
  git(
    dirname(barePath),
    '--git-dir',
    barePath,
    'update-ref',
    '-d',
    'refs/heads/feature',
  );
  git(
    dirname(barePath),
    '--git-dir',
    barePath,
    'symbolic-ref',
    'HEAD',
    'refs/heads/missing',
  );

  const targetPath = join(dataDir, 'repos', sourceId, refId);
  mkdirSync(dirname(targetPath), { recursive: true });
  renameSync(barePath, targetPath);
  return { mainHash, featureHash };
}

function seedRepoRow(db: ReturnType<typeof openDatabase>): void {
  db.prepare(
    `
    INSERT INTO repos (
      id, source_id, platform, ref_id, status, name, full_name, owner,
      description, html_url, clone_url, ssh_url, default_branch, visibility,
      is_private, is_fork, is_archived, origin, meta, last_seen_at,
      disabled_at, created_at, updated_at
    ) VALUES (
      1, 'source-a', 'github', '1', 'active', 'core', 'acme/core', 'acme',
      NULL, NULL, NULL, NULL, 'main', NULL,
      0, 0, 0, '{}', '{}', '2026-04-04T10:00:00Z',
      NULL, '2026-04-04T08:00:00Z', '2026-04-04T10:00:00Z'
    )
  `,
  ).run();
}

function seedRouteRef(
  db: ReturnType<typeof openDatabase>,
  refName: string,
  refKind: string,
  hash: string,
): void {
  db.prepare(
    `
    INSERT INTO repo_refs_current (
      repo_id, ref_name, ref_kind, current_hash, status, archive_ref_name,
      first_seen_at, last_seen_at, last_hash_updated_at, deleted_at,
      created_at, updated_at
    ) VALUES (
      1, ?, ?, ?, 'active', NULL,
      '2026-04-04T08:00:00Z', '2026-04-04T10:00:00Z',
      '2026-04-04T10:00:00Z', NULL,
      '2026-04-04T08:00:00Z', '2026-04-04T10:00:00Z'
    )
  `,
  ).run(refName, refKind, hash);
}

function git(cwd: string, ...args: Array<string>): void {
  execFileSync('git', args, { cwd, stdio: 'pipe' });
}

function gitOutput(cwd: string, ...args: Array<string>): string {
  return execFileSync('git', args, { cwd, encoding: 'utf8' }).trim();
}
