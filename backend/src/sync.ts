import { execFile, spawnSync } from 'node:child_process';
import { mkdirSync, statSync } from 'node:fs';
import { readdir, stat } from 'node:fs/promises';
import { join } from 'node:path';
import { promisify } from 'node:util';
import { minimatch } from 'minimatch';
import { decryptToken } from './crypto';
import { TOKEN_PASSPHRASE_ENV, loadConfigOrDefault } from './config-store';
import { asError, nowIso, sleep } from './util';
import type {
  AppDatabase,
  JsonRecord,
  RepoRefRow,
  RepoRow,
  SourceConfig,
} from './types';
import type { TaskContext } from './task-manager';

const execFileAsync = promisify(execFile);
const GITHUB_API_BASE = 'https://api.github.com';
const DEFAULT_PER_PAGE = 100;

type GitHubRepo = {
  id: number;
  name: string;
  full_name: string;
  description: string | null;
  html_url: string;
  clone_url: string;
  ssh_url: string;
  default_branch: string;
  visibility: string;
  private: boolean;
  fork: boolean;
  archived: boolean;
  language?: string | null;
  stargazers_count?: number;
  owner: {
    login: string;
    avatar_url?: string;
  };
};

type ResolvedRepo = {
  sourceId: string;
  platform: 'github';
  refId: string;
  name: string;
  fullName: string;
  owner: string;
  description: string;
  htmlUrl: string;
  cloneUrl: string;
  sshUrl: string;
  defaultBranch: string;
  visibility: string;
  isPrivate: boolean;
  isFork: boolean;
  isArchived: boolean;
  originKinds: Set<string>;
  meta: GitHubRepo;
};

type RemoteRef = {
  name: string;
  kind: 'head' | 'tag';
  hash: string;
};

type RefChange = {
  refName: string;
  refKind: 'head' | 'tag';
  oldHash: string;
  newHash: string;
  action: 'create' | 'update' | 'delete' | 'unchanged';
  archiveRefName: string;
};

type CommitInfo = {
  authoredAt: string | null;
  committedAt: string | null;
  authorName: string | null;
  authorEmail: string | null;
  message: string | null;
};

type RepoSyncOutcome = {
  changeCount: number;
  createdRefs: number;
  updatedRefs: number;
  deletedRefs: number;
  error?: Error;
};

export async function syncAllSources(
  dataDir: string,
  manager: {
    enqueueSource: (source: SourceConfig, parentTaskId: string) => void;
  },
  context: TaskContext,
): Promise<void> {
  await context.setProgress('Loading sources', { phase: 'load_sources' });
  const { config } = loadConfigOrDefault(dataDir);
  if (config.sources.length === 0) {
    await context.setProgress('No source configured', {
      phase: 'enqueue_sources',
      total: 0,
    });
    return;
  }
  let queued = 0;
  for (const [index, source] of config.sources.entries()) {
    await context.setProgress(`Queueing source ${source.name || source.id}`, {
      phase: 'enqueue_sources',
      index: index + 1,
      total: config.sources.length,
    });
    manager.enqueueSource(source, context.taskId);
    queued++;
  }
  await context.setProgress('Queued source sync tasks', {
    phase: 'done',
    total: config.sources.length,
    queued,
  });
}

export async function syncSource(
  dataDir: string,
  db: AppDatabase,
  sourceId: string,
  runId: string,
  context: TaskContext,
): Promise<void> {
  const { config } = loadConfigOrDefault(dataDir);
  const source = config.sources.find((candidate) => candidate.id === sourceId);
  if (!source) throw new Error(`source ${sourceId} was not found`);

  const resolvedSource = {
    ...source,
    token: decryptToken(source.token, process.env[TOKEN_PASSPHRASE_ENV] ?? ''),
  };
  await context.setProgress('Loaded source configuration', {
    phase: 'load_source',
    platform: resolvedSource.platform,
    include_defaults: resolvedSource.include_defaults,
    include_starred: resolvedSource.include_starred,
    include_watching: resolvedSource.include_watching,
  });

  const { repos, candidateTotal } = await resolveRepositories(
    resolvedSource,
    context,
  );
  const filtered = filterRepos(
    repos,
    source.only_include_repos,
    source.exclude_repos,
  );
  await context.setProgress(
    `Filtered repositories (${filtered.length} remaining)`,
    {
      phase: 'filter_repos',
      candidate_total: candidateTotal,
      filtered_total: filtered.length,
    },
  );

  const snapshot = persistRepoSnapshot(db, source.id, filtered);
  await context.setProgress('Persisted repository snapshot', {
    phase: 'persist_repos',
    ...snapshot,
  });

  const archive = await syncActiveRepos(
    dataDir,
    db,
    resolvedSource,
    runId,
    Math.max(config.concurrency, 1),
    Math.max(config.max_retry_times, 0),
    context,
  );
  await context.setProgress(`Archived ${archive.succeeded} repositories`, {
    phase: 'done',
    ...snapshot,
    archived_total: archive.succeeded,
    failed_total: archive.failed,
    change_count: archive.changeCount,
    created_ref_count: archive.createdRefs,
    updated_ref_count: archive.updatedRefs,
    deleted_ref_count: archive.deletedRefs,
  });
}

async function resolveRepositories(
  source: SourceConfig,
  context: TaskContext,
): Promise<{ repos: Array<ResolvedRepo>; candidateTotal: number }> {
  const reposById = new Map<string, ResolvedRepo>();
  const fetchers = [
    {
      enabled: source.include_defaults,
      kind: 'default',
      endpoint:
        '/user/repos?affiliation=owner,collaborator,organization_member',
    },
    {
      enabled: source.include_starred,
      kind: 'starred',
      endpoint: '/user/starred?',
    },
    {
      enabled: source.include_watching,
      kind: 'watching',
      endpoint: '/user/subscriptions?',
    },
  ];

  for (const fetcher of fetchers) {
    if (!fetcher.enabled) continue;
    for (let page = 1; ; page++) {
      const repos = await fetchGitHubRepos(
        source.token,
        fetcher.endpoint,
        page,
      );
      for (const repo of repos) {
        const refId = String(repo.id);
        const existing = reposById.get(refId);
        if (existing) {
          existing.originKinds.add(fetcher.kind);
          continue;
        }
        reposById.set(refId, {
          sourceId: source.id,
          platform: 'github',
          refId,
          name: repo.name,
          fullName: repo.full_name,
          owner: repo.owner.login,
          description: repo.description ?? '',
          htmlUrl: repo.html_url,
          cloneUrl: repo.clone_url,
          sshUrl: repo.ssh_url,
          defaultBranch: repo.default_branch,
          visibility: repo.visibility,
          isPrivate: repo.private,
          isFork: repo.fork,
          isArchived: repo.archived,
          originKinds: new Set([fetcher.kind]),
          meta: repo,
        });
      }
      await context.setProgress(`Fetching ${fetcher.kind} repositories`, {
        phase: `fetch_${fetcher.kind}`,
        page,
        page_repo_count: repos.length,
        discovered_total: reposById.size,
      });
      if (repos.length < DEFAULT_PER_PAGE) break;
    }
  }

  return {
    repos: [...reposById.values()].sort((a, b) =>
      a.fullName.localeCompare(b.fullName),
    ),
    candidateTotal: reposById.size,
  };
}

async function fetchGitHubRepos(
  token: string,
  endpoint: string,
  page: number,
): Promise<Array<GitHubRepo>> {
  const separator = endpoint.includes('?') ? '&' : '?';
  const response = await fetch(
    `${GITHUB_API_BASE}${endpoint}${separator}page=${page}&per_page=${DEFAULT_PER_PAGE}`,
    {
      headers: {
        Accept: 'application/vnd.github+json',
        Authorization: `Bearer ${token}`,
        'X-GitHub-Api-Version': '2022-11-28',
      },
    },
  );
  if (!response.ok) {
    const body = (await response.json().catch(() => ({}))) as {
      message?: string;
    };
    throw new Error(
      `github api returned ${response.status}: ${body.message ?? response.statusText}`,
    );
  }
  return (await response.json()) as Array<GitHubRepo>;
}

function filterRepos(
  repos: Array<ResolvedRepo>,
  onlyInclude: Array<string>,
  exclude: Array<string>,
): Array<ResolvedRepo> {
  return repos.filter((repo) => {
    const includeMatch =
      onlyInclude.length === 0 ||
      onlyInclude.some((pattern) => matchesRepoPattern(repo, pattern));
    const excludeMatch = exclude.some((pattern) =>
      matchesRepoPattern(repo, pattern),
    );
    return includeMatch && !excludeMatch;
  });
}

function matchesRepoPattern(repo: ResolvedRepo, pattern: string): boolean {
  return (
    minimatch(repo.fullName, pattern, { nocase: true }) ||
    minimatch(repo.name, pattern, { nocase: true })
  );
}

function persistRepoSnapshot(
  db: AppDatabase,
  sourceId: string,
  repos: Array<ResolvedRepo>,
) {
  const now = nowIso();
  const existing = db
    .prepare('SELECT * FROM repos WHERE source_id = ?')
    .all(sourceId) as Array<RepoRow>;
  const activeRefIds = new Set(repos.map((repo) => repo.refId));
  let inserted = 0;
  let updated = 0;
  let reactivated = 0;

  const upsert = db.prepare(`
    INSERT INTO repos (
      source_id, platform, ref_id, status, name, full_name, owner, description,
      html_url, clone_url, ssh_url, default_branch, visibility, is_private,
      is_fork, is_archived, origin, meta, archive_repo_size_bytes, last_seen_at,
      disabled_at, created_at, updated_at
    ) VALUES (
      @sourceId, @platform, @refId, 'active', @name, @fullName, @owner,
      @description, @htmlUrl, @cloneUrl, @sshUrl, @defaultBranch, @visibility,
      @isPrivate, @isFork, @isArchived, @origin, @meta, NULL, @now, NULL, @now, @now
    )
    ON CONFLICT(source_id, ref_id) DO UPDATE SET
      platform = excluded.platform,
      status = 'active',
      name = excluded.name,
      full_name = excluded.full_name,
      owner = excluded.owner,
      description = excluded.description,
      html_url = excluded.html_url,
      clone_url = excluded.clone_url,
      ssh_url = excluded.ssh_url,
      default_branch = excluded.default_branch,
      visibility = excluded.visibility,
      is_private = excluded.is_private,
      is_fork = excluded.is_fork,
      is_archived = excluded.is_archived,
      origin = excluded.origin,
      meta = excluded.meta,
      last_seen_at = excluded.last_seen_at,
      disabled_at = NULL,
      updated_at = excluded.updated_at
  `);

  const tx = db.transaction(() => {
    for (const repo of repos) {
      const existingRepo = existing.find((row) => row.ref_id === repo.refId);
      upsert.run({
        sourceId: repo.sourceId,
        platform: repo.platform,
        refId: repo.refId,
        name: repo.name,
        fullName: repo.fullName,
        owner: repo.owner,
        description: repo.description,
        htmlUrl: repo.htmlUrl,
        cloneUrl: repo.cloneUrl,
        sshUrl: repo.sshUrl,
        defaultBranch: repo.defaultBranch,
        visibility: repo.visibility,
        isPrivate: repo.isPrivate ? 1 : 0,
        isFork: repo.isFork ? 1 : 0,
        isArchived: repo.isArchived ? 1 : 0,
        origin: JSON.stringify({ kinds: [...repo.originKinds].sort() }),
        meta: JSON.stringify(repo.meta),
        now,
      });
      if (!existingRepo) inserted++;
      else if (existingRepo.status === 'auto_excluded') reactivated++;
      else updated++;
    }
    const mark = db.prepare(`
      UPDATE repos
      SET status = 'auto_excluded',
          disabled_at = COALESCE(disabled_at, ?),
          updated_at = ?
      WHERE id = ?
    `);
    for (const row of existing) {
      if (!activeRefIds.has(row.ref_id) && row.status !== 'auto_excluded') {
        mark.run(now, now, row.id);
      }
    }
  });
  tx();

  const autoExcluded = existing.filter(
    (row) => !activeRefIds.has(row.ref_id) && row.status !== 'auto_excluded',
  ).length;
  return {
    resolved_total: repos.length,
    inserted,
    updated,
    reactivated,
    auto_excluded: autoExcluded,
  };
}

async function syncActiveRepos(
  dataDir: string,
  db: AppDatabase,
  source: SourceConfig,
  runId: string,
  concurrency: number,
  maxRetryTimes: number,
  context: TaskContext,
) {
  const repos = db
    .prepare(
      "SELECT * FROM repos WHERE source_id = ? AND status = 'active' ORDER BY id",
    )
    .all(source.id) as Array<RepoRow>;
  await context.setProgress(`Loaded ${repos.length} active repositories`, {
    phase: 'load_active_repos',
    target_total: repos.length,
  });

  const result = {
    processed: 0,
    succeeded: 0,
    failed: 0,
    changeCount: 0,
    createdRefs: 0,
    updatedRefs: 0,
    deletedRefs: 0,
  };
  let cursor = 0;

  async function worker(): Promise<void> {
    for (;;) {
      const repo = repos[cursor++];
      if (!repo) return;
      const outcome = await syncRepoWithRetry(
        dataDir,
        db,
        source,
        runId,
        repo,
        maxRetryTimes,
      );
      result.processed++;
      if (outcome.error) {
        result.failed++;
        recordRepoSyncFailure(db, runId, repo, outcome.error);
      } else {
        result.succeeded++;
        result.changeCount += outcome.changeCount;
        result.createdRefs += outcome.createdRefs;
        result.updatedRefs += outcome.updatedRefs;
        result.deletedRefs += outcome.deletedRefs;
      }
      await context.setProgress(
        `Syncing active repositories (${result.processed}/${repos.length})`,
        {
          phase: 'sync_active_repos',
          target_total: repos.length,
          processed: result.processed,
          succeeded: result.succeeded,
          failed: result.failed,
          current_repo_id: repo.id,
          current_full_name: repo.full_name,
        },
      );
    }
  }

  await Promise.all(
    Array.from(
      { length: Math.min(concurrency, Math.max(repos.length, 1)) },
      () => worker(),
    ),
  );
  return result;
}

async function syncRepoWithRetry(
  dataDir: string,
  db: AppDatabase,
  source: SourceConfig,
  runId: string,
  repo: RepoRow,
  maxRetryTimes: number,
): Promise<RepoSyncOutcome> {
  let lastError: Error | undefined;
  for (let attempt = 0; attempt <= maxRetryTimes; attempt++) {
    try {
      return await syncRepoOnce(dataDir, db, source, runId, repo);
    } catch (error) {
      lastError = asError(error);
      if (attempt < maxRetryTimes) {
        await sleep(Math.min(120_000, 10_000 * 2 ** attempt));
      }
    }
  }
  return {
    changeCount: 0,
    createdRefs: 0,
    updatedRefs: 0,
    deletedRefs: 0,
    error: lastError,
  };
}

async function syncRepoOnce(
  dataDir: string,
  db: AppDatabase,
  source: SourceConfig,
  runId: string,
  repo: RepoRow,
): Promise<RepoSyncOutcome> {
  if (!repo.clone_url)
    throw new Error(`repo ${repo.full_name} has no clone URL`);
  const repoPath = join(dataDir, 'repos', source.id, repo.ref_id);
  await ensureBareRepo(repoPath, repo.clone_url);
  const currentRows = db
    .prepare(
      'SELECT * FROM repo_refs_current WHERE repo_id = ? ORDER BY ref_name',
    )
    .all(repo.id) as Array<RepoRefRow>;
  const remoteRefs = await listRemoteRefs(repo.clone_url, source.token);
  const changes = diffRefs(currentRows, remoteRefs);
  const fetchChanges = changes.filter(
    (change) => change.action === 'create' || change.action === 'update',
  );
  let archiveContentChanged = false;
  if (fetchChanges.length > 0) {
    await fetchArchiveRefs(repoPath, source.token, fetchChanges);
    archiveContentChanged = true;
  }
  const stats = persistRefState(
    db,
    repoPath,
    repo.id,
    runId,
    currentRows,
    remoteRefs,
    changes,
  );
  if (archiveContentChanged) {
    const size = await directorySize(repoPath);
    db.prepare(
      'UPDATE repos SET archive_repo_size_bytes = ?, updated_at = ? WHERE id = ?',
    ).run(size, nowIso(), repo.id);
  }
  return stats;
}

async function ensureBareRepo(path: string, remoteUrl: string): Promise<void> {
  mkdirSync(join(path, '..'), { recursive: true });
  if (!statExists(path)) {
    await runGit(process.cwd(), ['init', '--bare', path]);
  }
  await runGit(path, ['remote', 'remove', 'origin']).catch(() => undefined);
  await runGit(path, ['remote', 'add', 'origin', remoteUrl]);
}

async function listRemoteRefs(
  remoteUrl: string,
  token: string,
): Promise<Array<RemoteRef>> {
  const { stdout } = await runGit(
    process.cwd(),
    ['ls-remote', '--heads', '--tags', remoteUrl],
    token,
  );
  return stdout
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const [hash, name] = line.split(/\s+/);
      if (!hash || !name || name.endsWith('^{}')) return undefined;
      if (name.startsWith('refs/heads/'))
        return { name, hash, kind: 'head' as const };
      if (name.startsWith('refs/tags/'))
        return { name, hash, kind: 'tag' as const };
      return undefined;
    })
    .filter((ref): ref is RemoteRef => !!ref)
    .sort((a, b) => a.name.localeCompare(b.name));
}

async function fetchArchiveRefs(
  repoPath: string,
  token: string,
  changes: Array<RefChange>,
): Promise<void> {
  const refspecs = changes.map(
    (change) => `+${change.refName}:${change.archiveRefName}`,
  );
  await runGit(
    repoPath,
    ['fetch', '--no-tags', '--force', 'origin', ...refspecs],
    token,
  );
}

function diffRefs(
  currentRows: Array<RepoRefRow>,
  remoteRefs: Array<RemoteRef>,
): Array<RefChange> {
  const currentByName = new Map(
    currentRows
      .filter((row) => row.status === 'active')
      .map((row) => [row.ref_name, row]),
  );
  const remoteByName = new Map(remoteRefs.map((ref) => [ref.name, ref]));
  const names = [
    ...new Set([...currentByName.keys(), ...remoteByName.keys()]),
  ].sort();
  return names.map((name) => {
    const current = currentByName.get(name);
    const remote = remoteByName.get(name);
    if (!current && remote) {
      return {
        refName: name,
        refKind: remote.kind,
        oldHash: '',
        newHash: remote.hash,
        action: 'create',
        archiveRefName: archiveRefName(name, remote.hash),
      };
    }
    if (current && !remote) {
      return {
        refName: name,
        refKind: current.ref_kind,
        oldHash: current.current_hash,
        newHash: '',
        action: 'delete',
        archiveRefName: current.archive_ref_name ?? '',
      };
    }
    if (current && remote && current.current_hash !== remote.hash) {
      return {
        refName: name,
        refKind: remote.kind,
        oldHash: current.current_hash,
        newHash: remote.hash,
        action: 'update',
        archiveRefName: archiveRefName(name, remote.hash),
      };
    }
    return {
      refName: name,
      refKind: remote?.kind ?? current?.ref_kind ?? 'head',
      oldHash: current?.current_hash ?? '',
      newHash: remote?.hash ?? '',
      action: 'unchanged',
      archiveRefName: current?.archive_ref_name ?? '',
    };
  });
}

function persistRefState(
  db: AppDatabase,
  repoPath: string,
  repoId: number,
  runId: string,
  currentRows: Array<RepoRefRow>,
  remoteRefs: Array<RemoteRef>,
  changes: Array<RefChange>,
) {
  const now = nowIso();
  const currentByName = new Map(currentRows.map((row) => [row.ref_name, row]));
  const changeByName = new Map(
    changes.map((change) => [change.refName, change]),
  );
  const upsert = db.prepare(`
    INSERT INTO repo_refs_current (
      repo_id, ref_name, ref_kind, current_hash, status, archive_ref_name,
      first_seen_at, last_seen_at, last_hash_updated_at,
      current_commit_authored_at, current_commit_committed_at,
      current_commit_author_name, current_commit_author_email,
      current_commit_message, deleted_at, created_at, updated_at
    ) VALUES (
      @repoId, @refName, @refKind, @currentHash, 'active', @archiveRefName,
      @firstSeenAt, @lastSeenAt, @lastHashUpdatedAt,
      @commitAuthoredAt, @commitCommittedAt, @commitAuthorName, @commitAuthorEmail,
      @commitMessage, NULL, @now, @now
    )
    ON CONFLICT(repo_id, ref_name) DO UPDATE SET
      ref_kind = excluded.ref_kind,
      current_hash = excluded.current_hash,
      status = 'active',
      archive_ref_name = COALESCE(excluded.archive_ref_name, archive_ref_name),
      last_seen_at = excluded.last_seen_at,
      last_hash_updated_at = excluded.last_hash_updated_at,
      current_commit_authored_at = COALESCE(excluded.current_commit_authored_at, current_commit_authored_at),
      current_commit_committed_at = COALESCE(excluded.current_commit_committed_at, current_commit_committed_at),
      current_commit_author_name = COALESCE(excluded.current_commit_author_name, current_commit_author_name),
      current_commit_author_email = COALESCE(excluded.current_commit_author_email, current_commit_author_email),
      current_commit_message = COALESCE(excluded.current_commit_message, current_commit_message),
      deleted_at = NULL,
      updated_at = excluded.updated_at
  `);
  const markDeleted = db.prepare(`
    UPDATE repo_refs_current
    SET status = 'deleted', deleted_at = ?, last_seen_at = ?, updated_at = ?
    WHERE repo_id = ? AND ref_name = ?
  `);
  const insertChange = db.prepare(`
    INSERT OR IGNORE INTO repo_ref_changes (
      repo_id, task_run_id, ref_name, ref_kind, action, old_hash, new_hash,
      new_commit_authored_at, new_commit_committed_at, new_commit_author_name,
      new_commit_author_email, new_commit_message, archive_ref_name, created_at
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
  `);

  let createdRefs = 0;
  let updatedRefs = 0;
  let deletedRefs = 0;
  let changeCount = 0;

  const tx = db.transaction(() => {
    for (const remote of remoteRefs) {
      const current = currentByName.get(remote.name);
      const change = changeByName.get(remote.name);
      const needsCommit =
        change?.action === 'create' ||
        change?.action === 'update' ||
        !current?.current_commit_authored_at;
      const commit = needsCommit
        ? resolveCommitInfo(repoPath, remote.hash)
        : emptyCommit();
      const hashChanged = current?.current_hash !== remote.hash;
      upsert.run({
        repoId,
        refName: remote.name,
        refKind: remote.kind,
        currentHash: remote.hash,
        archiveRefName:
          change?.archiveRefName || current?.archive_ref_name || null,
        firstSeenAt: current?.first_seen_at ?? now,
        lastSeenAt: now,
        lastHashUpdatedAt: hashChanged
          ? now
          : (current?.last_hash_updated_at ?? now),
        commitAuthoredAt: commit.authoredAt,
        commitCommittedAt: commit.committedAt,
        commitAuthorName: commit.authorName,
        commitAuthorEmail: commit.authorEmail,
        commitMessage: commit.message,
        now,
      });
      if (change && change.action !== 'unchanged') {
        insertChange.run(
          repoId,
          runId,
          change.refName,
          change.refKind,
          change.action,
          change.oldHash || null,
          change.newHash || null,
          commit.authoredAt,
          commit.committedAt,
          commit.authorName,
          commit.authorEmail,
          commit.message,
          change.archiveRefName || null,
          now,
        );
        changeCount++;
        if (change.action === 'create') createdRefs++;
        if (change.action === 'update') updatedRefs++;
      }
    }
    for (const change of changes.filter(
      (candidate) => candidate.action === 'delete',
    )) {
      markDeleted.run(now, now, now, repoId, change.refName);
      insertChange.run(
        repoId,
        runId,
        change.refName,
        change.refKind,
        change.action,
        change.oldHash || null,
        null,
        null,
        null,
        null,
        null,
        null,
        change.archiveRefName || null,
        now,
      );
      changeCount++;
      deletedRefs++;
    }
  });
  tx();

  return { changeCount, createdRefs, updatedRefs, deletedRefs };
}

function resolveCommitInfo(repoPath: string, hash: string): CommitInfo {
  try {
    const target =
      runGitSync(repoPath, ['rev-list', '-n', '1', `${hash}^{}`]).trim() ||
      hash;
    const output = runGitSync(repoPath, [
      'show',
      '-s',
      '--format=%aI%x00%cI%x00%an%x00%ae%x00%B',
      target,
    ]);
    const [authoredAt, committedAt, authorName, authorEmail, ...messageParts] =
      output.split('\0');
    return {
      authoredAt: authoredAt || null,
      committedAt: committedAt || null,
      authorName: authorName || null,
      authorEmail: authorEmail || null,
      message: messageParts.join('\0').trim() || null,
    };
  } catch {
    return emptyCommit();
  }
}

function emptyCommit(): CommitInfo {
  return {
    authoredAt: null,
    committedAt: null,
    authorName: null,
    authorEmail: null,
    message: null,
  };
}

function archiveRefName(refName: string, hash: string): string {
  if (refName.startsWith('refs/heads/')) {
    return `refs/archive/heads/${refName.slice('refs/heads/'.length)}/${hash}`;
  }
  if (refName.startsWith('refs/tags/')) {
    return `refs/archive/tags/${refName.slice('refs/tags/'.length)}/${hash}`;
  }
  throw new Error(`unsupported ref name ${refName}`);
}

function recordRepoSyncFailure(
  db: AppDatabase,
  runId: string,
  repo: RepoRow,
  error: Error | undefined,
): void {
  db.prepare(
    `
      INSERT INTO task_run_logs (
        task_id, event_type, summary, meta_json, error_message, created_at
      ) VALUES (?, 'repo_sync_failed', ?, ?, ?, ?)
    `,
  ).run(
    runId,
    `Failed to sync ${repo.full_name}`,
    JSON.stringify({ repo_id: repo.id, full_name: repo.full_name }),
    error?.message ?? 'unknown error',
    nowIso(),
  );
}

async function runGit(
  cwd: string,
  args: Array<string>,
  token?: string,
): Promise<{ stdout: string; stderr: string }> {
  const authArgs = token
    ? ['-c', `http.extraHeader=Authorization: Bearer ${token}`]
    : [];
  const result = await execFileAsync('git', [...authArgs, ...args], {
    cwd,
    maxBuffer: 20 * 1024 * 1024,
  });
  return { stdout: result.stdout, stderr: result.stderr };
}

function runGitSync(cwd: string, args: Array<string>): string {
  const result = statSync(cwd);
  if (!result.isDirectory()) throw new Error(`${cwd} is not a directory`);
  const completed = spawnSync('git', args, { cwd, encoding: 'utf8' });
  if (completed.status !== 0) {
    throw new Error(completed.stderr || 'git command failed');
  }
  return completed.stdout;
}

function statExists(path: string): boolean {
  try {
    statSync(path);
    return true;
  } catch {
    return false;
  }
}

async function directorySize(path: string): Promise<number> {
  let total = 0;
  for (const entry of await readdir(path, { withFileTypes: true })) {
    const child = join(path, entry.name);
    if (entry.isDirectory()) {
      total += await directorySize(child);
    } else {
      total += (await stat(child)).size;
    }
  }
  return total;
}

export function repoToMeta(repo: RepoRow): JsonRecord {
  try {
    return JSON.parse(repo.meta) as JsonRecord;
  } catch {
    return {};
  }
}
