import { execFile, spawn } from 'node:child_process';
import { basename, join, posix as pathPosix } from 'node:path';
import { promisify } from 'node:util';
import { Code, ConnectError } from '@connectrpc/connect';
import {
  ConfigService,
  ValidationIssue_Severity,
} from '../../frontend/src/rpc/gitplus/config/v1/config_pb';
import { CronService } from '../../frontend/src/rpc/gitplus/cron/v1/cron_pb';
import { EventService } from '../../frontend/src/rpc/gitplus/event/v1/event_pb';
import { RepoService } from '../../frontend/src/rpc/gitplus/repo/v1/repo_pb';
import { TaskService } from '../../frontend/src/rpc/gitplus/task/v1/task_pb';
import {
  checkConfig,
  createSource,
  loadConfigOrDefault,
  saveConfig,
  toConfigSnapshot,
  toProtoSource,
  updateSource,
} from './config-store';
import { encryptToken } from './crypto';
import { streamRepositoryDownload } from './download';
import {
  JOB_ID_SYNC_ALL,
  JOB_TYPE_SYNC_ALL,
  JOB_TYPE_SYNC_SOURCE,
  toProtoTask,
} from './task-manager';
import { syncAllSources, syncSource } from './sync';
import {
  decodePageToken,
  encodePageToken,
  parseJsonObject,
  toTimestamp,
} from './util';
import type { DownloadManager } from './download';
import type { EventBus } from './event-bus';
import type { CronRuntime } from './cron-runtime';
import type { TaskManager } from './task-manager';
import type {
  BlameLine,
  CodeSearchMatch,
  CommitFileDiff,
  CommitInfo,
  CompareRefsResponse,
  GetBlobResponse,
  RepoRef,
  RepoRefChange,
  Repository,
  RepositoryCommit,
  RepositoryReadme,
  TreeEntry,
} from '../../frontend/src/rpc/gitplus/repo/v1/repo_pb';
import type { AppDatabase, RepoRefRow, RepoRow } from './types';
import type { JsonObject } from '@bufbuild/protobuf';
import type { ConnectRouter } from '@connectrpc/connect';
import type { FastifyReply, FastifyRequest } from 'fastify';

const execFileAsync = promisify(execFile);
const MAX_TEXT_BLOB_BYTES = 1024 * 1024;
const MAX_COMMIT_PATCH_BYTES = 2 * 1024 * 1024;
const MAX_BLAME_LINES = 5000;
const MAX_FILE_SEARCH_RESULTS = 100;
const MAX_CODE_SEARCH_RESULTS = 100;

export type RuntimeDeps = {
  dataDir: string;
  db: AppDatabase;
  bus: EventBus;
  tasks: TaskManager;
  downloads: DownloadManager;
  cron: CronRuntime;
};

export function createRoutes(
  deps: RuntimeDeps,
): (router: ConnectRouter) => void {
  return (router) => {
    registerConfig(router, deps);
    registerCron(router, deps);
    registerTasks(router, deps);
    registerRepos(router, deps);
    registerEvents(router, deps);
  };
}

export async function handleRawBlobRequest(
  deps: RuntimeDeps,
  request: FastifyRequest,
  reply: FastifyReply,
): Promise<void> {
  try {
    const params = request.params as { repoId?: string };
    const repoId = Number(params.repoId);
    if (!Number.isSafeInteger(repoId) || repoId <= 0) {
      reply.code(404).type('text/plain; charset=utf-8').send('not found\n');
      return;
    }

    const query = request.query as {
      ref?: unknown;
      path?: unknown;
      download?: unknown;
    };
    const repoPath = cleanRepositoryPath(queryValue(query.path));
    if (!repoPath) {
      reply
        .code(400)
        .type('text/plain; charset=utf-8')
        .send('path is required\n');
      return;
    }

    const repo = getRepo(deps.db, BigInt(repoId));
    const ref = chooseActiveRepoRef(
      repo,
      listRepoRefs(deps.db, repoId),
      queryValue(query.ref),
    );
    const archiveDir = archivePath(deps.dataDir, repo);
    const commitHash = await resolveCommitHash(archiveDir, ref.current_hash);
    const objectSpec = `${commitHash}:${repoPath}`;
    const objectType = await runGitText(archiveDir, [
      'cat-file',
      '-t',
      objectSpec,
    ]);
    if (objectType.trim() !== 'blob') {
      throw new ConnectError('file not found', Code.NotFound);
    }

    const size = Number(
      await runGitText(archiveDir, ['cat-file', '-s', objectSpec]),
    );
    const child = spawn(
      'git',
      ['--git-dir', archiveDir, 'cat-file', '-p', objectSpec],
      { stdio: ['ignore', 'pipe', 'pipe'] },
    );
    child.stderr.resume();
    child.on('error', () => {});

    reply.header('Content-Type', mediaTypeForPath(repoPath));
    if (Number.isFinite(size) && size >= 0) {
      reply.header('Content-Length', String(size));
    }
    reply.header(
      'Content-Disposition',
      rawBlobContentDisposition(basename(repoPath), queryFlag(query.download)),
    );
    await reply.send(child.stdout);
  } catch (error) {
    sendRawBlobError(reply, error);
  }
}

function registerConfig(router: ConnectRouter, deps: RuntimeDeps): void {
  router.service(ConfigService, {
    ping() {
      return {};
    },
    checkConfig() {
      const result = checkConfig(deps.dataDir);
      return {
        issues: result.issues,
        summary: summarizeIssues(result.issues),
      };
    },
    checkSourceConfig(req) {
      const sourceId = req.sourceId.trim();
      const result = checkConfig(deps.dataDir, sourceId);
      return {
        exists:
          result.exists &&
          result.issues.every((issue) => issue.code !== 'source_not_found'),
        sourceId,
        issues: result.issues,
        summary: summarizeIssues(result.issues),
      };
    },
    getConfig() {
      const { config, exists } = loadConfigOrDefault(deps.dataDir);
      return { exists, config: toConfigSnapshot(config) };
    },
    updateConfig(req) {
      const { config } = loadConfigOrDefault(deps.dataDir);
      config.concurrency =
        req.concurrency > 0 ? req.concurrency : config.concurrency;
      config.max_retry_times =
        req.maxRetryTimes >= 0 ? req.maxRetryTimes : config.max_retry_times;
      saveConfig(deps.dataDir, config);
      return { config: toConfigSnapshot(config) };
    },
    createSource(req) {
      if (!req.source)
        throw new ConnectError('source is required', Code.InvalidArgument);
      const { config } = loadConfigOrDefault(deps.dataDir);
      const source = createSource(req.source);
      while (config.sources.some((candidate) => candidate.id === source.id)) {
        source.id = createSource(req.source).id;
      }
      config.sources.push(source);
      saveConfig(deps.dataDir, config);
      return { config: toConfigSnapshot(config) };
    },
    updateSource(req) {
      const { config } = loadConfigOrDefault(deps.dataDir);
      const index = config.sources.findIndex(
        (source) => source.id === req.sourceId,
      );
      if (index < 0) {
        throw new ConnectError(
          `source ${req.sourceId} was not found`,
          Code.NotFound,
        );
      }
      const patch = req.patch;
      if (!patch)
        throw new ConnectError('patch is required', Code.InvalidArgument);
      config.sources[index] = updateSource(config.sources[index], {
        ...patch,
        onlyIncludeRepos: patch.onlyIncludeRepos?.values,
        excludeRepos: patch.excludeRepos?.values,
      });
      saveConfig(deps.dataDir, config);
      return { config: toConfigSnapshot(config) };
    },
    replaceSourceToken(req) {
      const { config } = loadConfigOrDefault(deps.dataDir);
      const source = config.sources.find(
        (candidate) => candidate.id === req.sourceId,
      );
      if (!source)
        throw new ConnectError(
          `source ${req.sourceId} was not found`,
          Code.NotFound,
        );
      source.token = encryptToken(
        req.tokenPlaintext,
        process.env.ENCRYPTION_PASSPHRASE ?? '',
      );
      saveConfig(deps.dataDir, config);
      return { source: toProtoSource(source) };
    },
    deleteSource(req) {
      const { config } = loadConfigOrDefault(deps.dataDir);
      const nextSources = config.sources.filter(
        (source) => source.id !== req.sourceId,
      );
      if (nextSources.length === config.sources.length) {
        throw new ConnectError(
          `source ${req.sourceId} was not found`,
          Code.NotFound,
        );
      }
      config.sources = nextSources;
      saveConfig(deps.dataDir, config);
      return { config: toConfigSnapshot(config) };
    },
  });
}

function registerCron(router: ConnectRouter, deps: RuntimeDeps): void {
  router.service(CronService, {
    getCronRuntime() {
      return { runtime: deps.cron.snapshot() };
    },
    updateCron(req) {
      const { config } = loadConfigOrDefault(deps.dataDir);
      config.cron = req.cron.trim();
      saveConfig(deps.dataDir, config);
      return { runtime: deps.cron.reload() };
    },
    reloadCron() {
      return { runtime: deps.cron.reload() };
    },
  });
}

function registerTasks(router: ConnectRouter, deps: RuntimeDeps): void {
  router.service(TaskService, {
    getTaskRuntime() {
      const runtime = deps.tasks.runtime();
      return {
        runningTask: runtime.runningTask
          ? toProtoTask(runtime.runningTask)
          : undefined,
        queuedTasks: runtime.queuedTasks.map(toProtoTask),
      };
    },
    listTaskRuns(req) {
      const pageSize = normalizePageSize(req.pageSize);
      const offset = decodePageToken(req.pageToken);
      const result = deps.tasks.listTaskRuns(
        pageSize,
        offset,
        req.jobType,
        req.parentTaskId,
      );
      return {
        taskRuns: result.tasks,
        totalCount: result.totalCount,
        nextPageToken:
          offset + result.tasks.length < result.totalCount
            ? encodePageToken(offset + result.tasks.length)
            : '',
      };
    },
    getTaskRun(req) {
      return { taskRun: deps.tasks.getTaskRun(req.taskId) };
    },
    listTaskRunLogs(req) {
      return { logs: deps.tasks.listTaskRunLogs(req.taskId) };
    },
    enqueueFullSync() {
      const result = enqueueFullSyncTask(deps);
      return { result: result.result, task: toProtoTask(result.task) };
    },
    enqueueSourceSync(req) {
      const result = enqueueSourceTask(deps, req.sourceId);
      return { result: result.result, task: toProtoTask(result.task) };
    },
    cancelQueuedTask(req) {
      return { task: toProtoTask(deps.tasks.cancelQueuedTask(req.taskId)) };
    },
  });
}

export function enqueueFullSyncTask(deps: Omit<RuntimeDeps, 'cron'>) {
  return deps.tasks.enqueue({
    jobId: JOB_ID_SYNC_ALL,
    jobType: JOB_TYPE_SYNC_ALL,
    name: 'Sync all sources',
    run: (context) =>
      syncAllSources(
        deps.dataDir,
        {
          enqueueSource: (source, parentTaskId) => {
            enqueueSourceTask(deps, source.id, parentTaskId);
          },
        },
        context,
      ),
  });
}

function enqueueSourceTask(
  deps: Omit<RuntimeDeps, 'cron'>,
  sourceId: string,
  parentTaskId = '',
) {
  const { config } = loadConfigOrDefault(deps.dataDir);
  const source = config.sources.find((candidate) => candidate.id === sourceId);
  if (!source)
    throw new ConnectError(`source ${sourceId} was not found`, Code.NotFound);
  return deps.tasks.enqueue({
    parentTaskId,
    jobId: `sync-source:${source.id}`,
    jobType: JOB_TYPE_SYNC_SOURCE,
    name: `Sync source ${source.name || source.id}`,
    args: { source_id: source.id },
    run: (context) =>
      syncSource(deps.dataDir, deps.db, source.id, context.taskId, context),
  });
}

function registerRepos(router: ConnectRouter, deps: RuntimeDeps): void {
  router.service(RepoService, {
    listRepositories(req) {
      const pageSize = normalizePageSize(req.pageSize);
      const offset = decodePageToken(req.pageToken);
      const filters = buildRepoFilters(req.search, req.sourceId);
      const total = deps.db
        .prepare(`SELECT COUNT(1) AS count FROM repos ${filters.where}`)
        .get(filters.params) as { count: number };
      const sort = repoSort(req.sort);
      const rows = deps.db
        .prepare(
          `
          SELECT * FROM repos
          ${filters.where}
          ORDER BY ${sort}
          LIMIT @limit OFFSET @offset
        `,
        )
        .all({ ...filters.params, limit: pageSize, offset }) as Array<RepoRow>;
      return {
        repositories: rows.map(repoToProto),
        totalCount: total.count,
        nextPageToken:
          offset + rows.length < total.count
            ? encodePageToken(offset + rows.length)
            : '',
      };
    },
    getRepository(req) {
      const repo = getRepo(deps.db, req.id);
      return { repository: repoToProto(repo) };
    },
    streamRepositoryDownload(req) {
      return streamRepositoryDownload(
        deps.dataDir,
        deps.db,
        deps.downloads,
        req.repoId,
      );
    },
    listRefs(req) {
      const rows = deps.db
        .prepare(
          `
          SELECT * FROM repo_refs_current
          WHERE repo_id = ?
            AND ref_kind = ?
            AND (? = 1 OR status = 'active')
          ORDER BY ref_name
        `,
        )
        .all(
          Number(req.repoId),
          req.refKind,
          req.includeDeleted ? 1 : 0,
        ) as Array<RepoRefRow>;
      return { refs: rows.map(refToProto) };
    },
    listRefChanges(req) {
      const pageSize = normalizePageSize(req.pageSize);
      const offset = decodePageToken(req.pageToken);
      const refName = req.refName.trim();
      const params = {
        repoId: Number(req.repoId),
        refName: refName || null,
        limit: pageSize,
        offset,
      };
      const total = deps.db
        .prepare(
          `
          SELECT COUNT(1) AS count
          FROM repo_ref_changes
          WHERE repo_id = @repoId
            AND (@refName IS NULL OR ref_name = @refName)
        `,
        )
        .get(params) as { count: number };
      const rows = deps.db
        .prepare(
          `
          SELECT * FROM repo_ref_changes
          WHERE repo_id = @repoId
            AND (@refName IS NULL OR ref_name = @refName)
          ORDER BY created_at DESC, id DESC
          LIMIT @limit OFFSET @offset
        `,
        )
        .all(params) as Array<RepoRefChangeRow>;
      return {
        changes: rows.map(changeToProto),
        totalCount: total.count,
        nextPageToken:
          offset + rows.length < total.count
            ? encodePageToken(offset + rows.length)
            : '',
      };
    },
    async listTree(req) {
      const repoId = normalizeRepoId(req.repoId);
      const repo = getRepo(deps.db, req.repoId);
      const ref = chooseActiveRepoRef(
        repo,
        listRepoRefs(deps.db, repoId),
        req.refName,
      );
      const archiveDir = archivePath(deps.dataDir, repo);
      const commitHash = await resolveCommitHash(archiveDir, ref.current_hash);
      const repoPath = cleanRepositoryPath(req.path);
      const entries = await listTreeEntries(archiveDir, commitHash, repoPath);
      return {
        repoId: BigInt(repoId),
        refName: ref.ref_name,
        commitHash,
        path: repoPath,
        entries,
        readme: await readRepositoryReadme(archiveDir, commitHash, entries),
      };
    },
    async searchFiles(req) {
      const repoId = normalizeRepoId(req.repoId);
      const repo = getRepo(deps.db, req.repoId);
      const ref = chooseActiveRepoRef(
        repo,
        listRepoRefs(deps.db, repoId),
        req.refName,
      );
      const archiveDir = archivePath(deps.dataDir, repo);
      const commitHash = await resolveCommitHash(archiveDir, ref.current_hash);
      const result = await searchFileEntries(
        archiveDir,
        commitHash,
        req.query,
        req.pageSize,
      );
      return {
        repoId: BigInt(repoId),
        refName: ref.ref_name,
        commitHash,
        entries: result.entries,
        isTruncated: result.isTruncated,
      };
    },
    async searchCode(req) {
      const repoId = normalizeRepoId(req.repoId);
      const repo = getRepo(deps.db, req.repoId);
      const ref = chooseActiveRepoRef(
        repo,
        listRepoRefs(deps.db, repoId),
        req.refName,
      );
      const archiveDir = archivePath(deps.dataDir, repo);
      const commitHash = await resolveCommitHash(archiveDir, ref.current_hash);
      const result = await searchCodeMatches(
        archiveDir,
        commitHash,
        req.query,
        req.pageSize,
      );
      return {
        repoId: BigInt(repoId),
        refName: ref.ref_name,
        commitHash,
        matches: result.matches,
        isTruncated: result.isTruncated,
      };
    },
    async getBlob(req) {
      const repoId = normalizeRepoId(req.repoId);
      const repoPath = cleanRepositoryPath(req.path);
      if (!repoPath) {
        throw new ConnectError('path is required', Code.InvalidArgument);
      }

      const repo = getRepo(deps.db, req.repoId);
      const ref = chooseActiveRepoRef(
        repo,
        listRepoRefs(deps.db, repoId),
        req.refName,
      );
      const archiveDir = archivePath(deps.dataDir, repo);
      const commitHash = await resolveCommitHash(archiveDir, ref.current_hash);
      return readBlobResponse(
        archiveDir,
        commitHash,
        repoId,
        ref.ref_name,
        repoPath,
      );
    },
    async getBlame(req) {
      const repoId = normalizeRepoId(req.repoId);
      const repoPath = cleanRepositoryPath(req.path);
      if (!repoPath) {
        throw new ConnectError('path is required', Code.InvalidArgument);
      }

      const repo = getRepo(deps.db, req.repoId);
      const ref = chooseActiveRepoRef(
        repo,
        listRepoRefs(deps.db, repoId),
        req.refName,
      );
      const archiveDir = archivePath(deps.dataDir, repo);
      const commitHash = await resolveCommitHash(archiveDir, ref.current_hash);
      const blame = await getBlame(archiveDir, commitHash, repoPath);
      return {
        repoId: BigInt(repoId),
        refName: ref.ref_name,
        commitHash,
        path: repoPath,
        lines: blame.lines,
        isTruncated: blame.isTruncated,
      };
    },
    async listCommits(req) {
      const repoId = normalizeRepoId(req.repoId);
      const pageSize = normalizePageSize(req.pageSize);
      const offset = decodePageToken(req.pageToken);
      const pathFilter = cleanRepositoryPath(req.path);
      const repo = getRepo(deps.db, req.repoId);
      const ref = chooseActiveRepoRef(
        repo,
        listRepoRefs(deps.db, repoId),
        req.refName,
      );
      const archiveDir = archivePath(deps.dataDir, repo);
      const commitHash = await resolveCommitHash(archiveDir, ref.current_hash);
      const commits = await listCommits(
        archiveDir,
        commitHash,
        pageSize + 1,
        offset,
        pathFilter,
      );
      const hasNext = commits.length > pageSize;
      return {
        commits: hasNext ? commits.slice(0, pageSize) : commits,
        nextPageToken: hasNext ? encodePageToken(offset + pageSize) : '',
      };
    },
    async getCommit(req) {
      const repoId = normalizeRepoId(req.repoId);
      const commitHash = req.commitHash.trim();
      if (!commitHash) {
        throw new ConnectError('commit_hash is required', Code.InvalidArgument);
      }

      const repo = getRepo(deps.db, req.repoId);
      const archiveDir = archivePath(deps.dataDir, repo);
      const resolvedHash = await resolveCommitHash(archiveDir, commitHash);
      const commit = await getCommit(archiveDir, resolvedHash);
      const { patch, isTruncated } = await getCommitPatch(
        archiveDir,
        resolvedHash,
      );
      const { files, additions, deletions } = parseCommitPatch(
        patch,
        isTruncated,
      );

      void repoId;
      return {
        commit,
        files,
        additions,
        deletions,
        isTruncated,
      };
    },
    async compareRefs(req) {
      const repoId = normalizeRepoId(req.repoId);
      const pageSize = normalizePageSize(req.pageSize);
      if (!req.baseRefName.trim()) {
        throw new ConnectError(
          'base_ref_name is required',
          Code.InvalidArgument,
        );
      }
      if (!req.headRefName.trim()) {
        throw new ConnectError(
          'head_ref_name is required',
          Code.InvalidArgument,
        );
      }

      const repo = getRepo(deps.db, req.repoId);
      const refs = listRepoRefs(deps.db, repoId);
      const baseRef = chooseActiveRepoRef(repo, refs, req.baseRefName);
      const headRef = chooseActiveRepoRef(repo, refs, req.headRefName);
      const archiveDir = archivePath(deps.dataDir, repo);
      const baseCommitHash = await resolveCommitHash(
        archiveDir,
        baseRef.current_hash,
      );
      const headCommitHash = await resolveCommitHash(
        archiveDir,
        headRef.current_hash,
      );
      const [aheadCount, behindCount, mergeBaseHash, commits, diff] =
        await Promise.all([
          countRevisionRange(archiveDir, baseCommitHash, headCommitHash),
          countRevisionRange(archiveDir, headCommitHash, baseCommitHash),
          findMergeBase(archiveDir, baseCommitHash, headCommitHash),
          listCompareCommits(
            archiveDir,
            baseCommitHash,
            headCommitHash,
            pageSize,
          ),
          getRevisionDiffPatch(archiveDir, baseCommitHash, headCommitHash),
        ]);
      const { files, additions, deletions } = parseCommitPatch(
        diff.patch,
        diff.isTruncated,
      );

      return {
        baseRefName: baseRef.ref_name,
        headRefName: headRef.ref_name,
        baseCommitHash,
        headCommitHash,
        mergeBaseHash,
        aheadCount,
        behindCount,
        commits,
        files,
        additions,
        deletions,
        isTruncated: diff.isTruncated,
      } as CompareRefsResponse;
    },
  });
}

function registerEvents(router: ConnectRouter, deps: RuntimeDeps): void {
  router.service(EventService, {
    async *subscribe(req, context) {
      for await (const event of deps.bus.subscribe(
        req.channel,
        context.signal,
      )) {
        yield { event: event as JsonObject };
      }
    },
  });
}

function summarizeIssues(
  issues: Array<{ severity: ValidationIssue_Severity }>,
) {
  return {
    error: issues.filter(
      (issue) => issue.severity === ValidationIssue_Severity.ERROR,
    ).length,
    warning: issues.filter(
      (issue) => issue.severity === ValidationIssue_Severity.WARNING,
    ).length,
    info: issues.filter(
      (issue) => issue.severity === ValidationIssue_Severity.INFO,
    ).length,
  };
}

function normalizePageSize(value: number): number {
  if (!Number.isInteger(value) || value <= 0) return 20;
  return Math.min(value, 100);
}

function buildRepoFilters(search: string, sourceId: string) {
  const where: Array<string> = [];
  const params: Record<string, string | null> = {};
  if (sourceId.trim()) {
    where.push('source_id = @sourceId');
    params.sourceId = sourceId.trim();
  }
  if (search.trim()) {
    where.push('(full_name LIKE @search OR description LIKE @search)');
    params.search = `%${search.trim()}%`;
  }
  return {
    where: where.length > 0 ? `WHERE ${where.join(' AND ')}` : '',
    params,
  };
}

function repoSort(sort: string): string {
  switch (sort) {
    case 'created_at_asc':
      return 'created_at ASC';
    case 'name_asc':
      return 'name ASC';
    case 'name_desc':
      return 'name DESC';
    default:
      return 'created_at DESC';
  }
}

function getRepo(db: AppDatabase, id: bigint): RepoRow {
  const repoId = Number(id);
  const row = db.prepare('SELECT * FROM repos WHERE id = ?').get(repoId) as
    | RepoRow
    | undefined;
  if (!row) throw new ConnectError('repository not found', Code.NotFound);
  return row;
}

function normalizeRepoId(id: bigint): number {
  const repoId = Number(id);
  if (!Number.isSafeInteger(repoId) || repoId <= 0) {
    throw new ConnectError('repo_id is required', Code.InvalidArgument);
  }
  return repoId;
}

function listRepoRefs(db: AppDatabase, repoId: number): Array<RepoRefRow> {
  return db
    .prepare(
      `
      SELECT * FROM repo_refs_current
      WHERE repo_id = ?
      ORDER BY ref_kind, ref_name
    `,
    )
    .all(repoId) as Array<RepoRefRow>;
}

function chooseActiveRepoRef(
  repo: RepoRow,
  refs: Array<RepoRefRow>,
  requestedRef: string,
): RepoRefRow {
  const activeRefs = refs.filter((ref) => ref.status === 'active');
  const trimmed = requestedRef.trim();
  if (trimmed) {
    for (const candidate of requestedRefCandidates(trimmed)) {
      const matched = activeRefs.find((ref) => ref.ref_name === candidate);
      if (matched) return matched;
    }
    if (isCommitHashLike(trimmed)) {
      return syntheticCommitRef(repo, trimmed);
    }
    throw new ConnectError('repository ref not found', Code.NotFound);
  }

  if (repo.default_branch) {
    const defaultRef = activeRefs.find(
      (ref) => ref.ref_name === `refs/heads/${repo.default_branch}`,
    );
    if (defaultRef) return defaultRef;
  }

  const firstBranch = activeRefs.find((ref) => ref.ref_kind === 'head');
  if (firstBranch) return firstBranch;
  const firstRef = activeRefs[0];
  if (firstRef) return firstRef;
  throw new ConnectError('repository ref not found', Code.NotFound);
}

function requestedRefCandidates(refName: string): Array<string> {
  if (refName.startsWith('refs/')) return [refName];
  return [`refs/heads/${refName}`, `refs/tags/${refName}`, refName];
}

function isCommitHashLike(value: string): boolean {
  return /^[0-9a-f]{7,40}$/i.test(value.trim());
}

function syntheticCommitRef(repo: RepoRow, commitHash: string): RepoRefRow {
  const now = new Date(0).toISOString();
  return {
    id: 0,
    repo_id: repo.id,
    ref_name: commitHash,
    ref_kind: 'head',
    current_hash: commitHash,
    status: 'active',
    archive_ref_name: null,
    first_seen_at: now,
    last_seen_at: now,
    last_hash_updated_at: now,
    current_commit_authored_at: null,
    current_commit_committed_at: null,
    current_commit_author_name: null,
    current_commit_author_email: null,
    current_commit_message: null,
    deleted_at: null,
    created_at: now,
    updated_at: now,
  };
}

function archivePath(dataDir: string, repo: RepoRow): string {
  return join(dataDir, 'repos', repo.source_id, repo.ref_id);
}

function queryValue(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

function queryFlag(value: unknown): boolean {
  return value === '1' || value === 'true' || value === 'yes';
}

function mediaTypeForPath(repoPath: string): string {
  const extension = repoPath.toLowerCase().split('.').pop() ?? '';
  switch (extension) {
    case 'apng':
      return 'image/apng';
    case 'avif':
      return 'image/avif';
    case 'bmp':
      return 'image/bmp';
    case 'gif':
      return 'image/gif';
    case 'ico':
      return 'image/x-icon';
    case 'jpeg':
    case 'jpg':
      return 'image/jpeg';
    case 'png':
      return 'image/png';
    case 'svg':
      return 'image/svg+xml';
    case 'webp':
      return 'image/webp';
    case 'css':
      return 'text/css; charset=utf-8';
    case 'csv':
      return 'text/csv; charset=utf-8';
    case 'htm':
    case 'html':
      return 'text/html; charset=utf-8';
    case 'js':
    case 'mjs':
    case 'cjs':
    case 'jsx':
    case 'ts':
    case 'tsx':
      return 'text/plain; charset=utf-8';
    case 'json':
    case 'map':
      return 'application/json; charset=utf-8';
    case 'md':
    case 'markdown':
      return 'text/markdown; charset=utf-8';
    case 'txt':
    case 'text':
    case 'log':
    case 'go':
    case 'rs':
    case 'py':
    case 'rb':
    case 'java':
    case 'c':
    case 'cc':
    case 'cpp':
    case 'h':
    case 'hpp':
    case 'xml':
    case 'yaml':
    case 'yml':
      return 'text/plain; charset=utf-8';
    case 'pdf':
      return 'application/pdf';
    default:
      return 'application/octet-stream';
  }
}

function rawBlobContentDisposition(
  filename: string,
  download: boolean,
): string {
  const disposition = download ? 'attachment' : 'inline';
  return `${disposition}; filename="${filename.replaceAll('"', '')}"`;
}

function sendRawBlobError(reply: FastifyReply, error: unknown): void {
  if (reply.sent) return;

  if (error instanceof ConnectError) {
    switch (error.code) {
      case Code.InvalidArgument:
        reply
          .code(400)
          .type('text/plain; charset=utf-8')
          .send(`${error.message}\n`);
        return;
      case Code.NotFound:
        reply
          .code(404)
          .type('text/plain; charset=utf-8')
          .send(`${error.message}\n`);
        return;
      default:
        reply
          .code(500)
          .type('text/plain; charset=utf-8')
          .send(`${error.message}\n`);
        return;
    }
  }

  reply
    .code(500)
    .type('text/plain; charset=utf-8')
    .send(
      `${error instanceof Error ? error.message : 'failed to read file'}\n`,
    );
}

function cleanRepositoryPath(value: string): string {
  const trimmed = value.trim().replaceAll('\\', '/');
  if (!trimmed || trimmed === '.' || trimmed === '/') return '';
  if (trimmed.includes('\0')) {
    throw new ConnectError(
      'path contains invalid character',
      Code.InvalidArgument,
    );
  }
  const cleaned = pathPosix.normalize(`/${trimmed}`).replace(/^\/+/, '');
  return cleaned === '.' ? '' : cleaned;
}

async function resolveCommitHash(
  gitDir: string,
  refHash: string,
): Promise<string> {
  try {
    return (
      await runGitText(gitDir, ['rev-parse', `${refHash}^{commit}`])
    ).trim();
  } catch (error) {
    throw new ConnectError(
      error instanceof Error ? error.message : 'commit object not found',
      Code.NotFound,
    );
  }
}

async function listTreeEntries(
  gitDir: string,
  commitHash: string,
  repoPath: string,
): Promise<Array<TreeEntry>> {
  const treeish = repoPath ? `${commitHash}:${repoPath}` : commitHash;
  let stdout: Buffer;
  try {
    stdout = await runGitBuffer(gitDir, ['ls-tree', '-z', '-l', treeish]);
  } catch (error) {
    throw new ConnectError(
      error instanceof Error ? error.message : 'directory not found',
      Code.NotFound,
    );
  }

  return stdout
    .toString('utf8')
    .split('\0')
    .filter(Boolean)
    .map((record) => parseTreeEntry(record, repoPath))
    .sort((left, right) => {
      if (left.kind === 'directory' && right.kind !== 'directory') return -1;
      if (left.kind !== 'directory' && right.kind === 'directory') return 1;
      return left.name.localeCompare(right.name, undefined, {
        sensitivity: 'base',
      });
    });
}

function parseTreeEntry(record: string, parentPath: string): TreeEntry {
  const tabIndex = record.indexOf('\t');
  if (tabIndex < 0) {
    throw new ConnectError('invalid git tree entry', Code.Internal);
  }
  const meta = record.slice(0, tabIndex);
  const name = record.slice(tabIndex + 1);
  const match = /^(\d{6}) (\w+) ([0-9a-f]{40}) +(-|\d+)$/.exec(meta);
  if (!match) {
    throw new ConnectError('invalid git tree metadata', Code.Internal);
  }

  const [, mode, objectType, hash, rawSize] = match;
  const entryPath = parentPath ? pathPosix.join(parentPath, name) : name;
  const kind = treeEntryKind(mode, objectType);

  return {
    name,
    path: entryPath,
    kind,
    mode,
    hash,
    size: rawSize === '-' ? 0n : BigInt(rawSize),
  } as TreeEntry;
}

async function searchFileEntries(
  gitDir: string,
  commitHash: string,
  query: string,
  pageSize: number,
): Promise<{ entries: Array<TreeEntry>; isTruncated: boolean }> {
  const limit =
    Number.isInteger(pageSize) && pageSize > 0
      ? Math.min(pageSize, MAX_FILE_SEARCH_RESULTS)
      : MAX_FILE_SEARCH_RESULTS;
  const terms = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  const stdout = await runGitBuffer(gitDir, [
    'ls-tree',
    '-r',
    '-z',
    '-l',
    commitHash,
  ]);
  const matches: Array<TreeEntry> = [];
  let isTruncated = false;

  for (const record of stdout.toString('utf8').split('\0')) {
    if (!record) continue;
    const entry = parseTreeEntry(record, '');
    if (entry.kind === 'directory') continue;
    const haystack = entry.path.toLowerCase();
    if (terms.length > 0 && !terms.every((term) => haystack.includes(term))) {
      continue;
    }
    if (matches.length >= limit) {
      isTruncated = true;
      break;
    }
    matches.push({
      ...entry,
      name: basename(entry.path),
    } as TreeEntry);
  }

  return { entries: matches, isTruncated };
}

async function searchCodeMatches(
  gitDir: string,
  commitHash: string,
  query: string,
  pageSize: number,
): Promise<{ matches: Array<CodeSearchMatch>; isTruncated: boolean }> {
  const trimmed = query.trim();
  if (!trimmed) return { matches: [], isTruncated: false };

  const limit =
    Number.isInteger(pageSize) && pageSize > 0
      ? Math.min(pageSize, MAX_CODE_SEARCH_RESULTS)
      : MAX_CODE_SEARCH_RESULTS;

  let stdout = '';
  try {
    const result = (await execFileAsync(
      'git',
      [
        '--git-dir',
        gitDir,
        'grep',
        '-n',
        '-I',
        '-F',
        '-e',
        trimmed,
        commitHash,
        '--',
      ],
      {
        encoding: 'utf8',
        maxBuffer: 20 * 1024 * 1024,
      },
    )) as { stdout: string };
    stdout = result.stdout;
  } catch (error) {
    if (isGitNoMatches(error)) {
      return { matches: [], isTruncated: false };
    }
    throw new ConnectError(
      error instanceof Error ? error.message : 'code search failed',
      Code.Internal,
    );
  }

  const matches: Array<CodeSearchMatch> = [];
  let isTruncated = false;
  for (const rawLine of stdout.split('\n')) {
    if (!rawLine) continue;
    const match = parseGitGrepLine(rawLine, commitHash);
    if (!match) continue;
    if (matches.length >= limit) {
      isTruncated = true;
      break;
    }
    matches.push(match);
  }

  return { matches, isTruncated };
}

function isGitNoMatches(error: unknown): boolean {
  return isGitExitCode(error, 1);
}

function isGitExitCode(error: unknown, code: number): boolean {
  return (
    typeof error === 'object' &&
    error !== null &&
    'code' in error &&
    (error as { code?: unknown }).code === code
  );
}

function parseGitGrepLine(
  rawLine: string,
  commitHash: string,
): CodeSearchMatch | null {
  const prefix = `${commitHash}:`;
  const line = rawLine.startsWith(prefix)
    ? rawLine.slice(prefix.length)
    : rawLine;
  const match = /^(.+?):(\d+):(.*)$/.exec(line);
  if (!match) return null;
  return {
    path: match[1],
    lineNo: Number(match[2]),
    line: match[3],
  } as CodeSearchMatch;
}

function treeEntryKind(mode: string, objectType: string): string {
  if (objectType === 'tree') return 'directory';
  if (objectType === 'commit') return 'submodule';
  if (mode === '120000') return 'symlink';
  return 'file';
}

async function readRepositoryReadme(
  gitDir: string,
  commitHash: string,
  entries: Array<TreeEntry>,
): Promise<RepositoryReadme | undefined> {
  const priority = new Map([
    ['readme.md', 0],
    ['readme.markdown', 1],
    ['readme.mdown', 2],
    ['readme.mkdn', 3],
  ]);
  const readme = entries
    .filter((entry) => entry.kind === 'file')
    .map((entry) => ({
      entry,
      priority:
        priority.get(entry.name.toLowerCase()) ?? Number.POSITIVE_INFINITY,
    }))
    .filter((item) => Number.isFinite(item.priority))
    .sort((left, right) => left.priority - right.priority)[0]?.entry;
  if (!readme) return undefined;

  const preview = await readBlobPreview(
    gitDir,
    `${commitHash}:${readme.path}`,
    Number(readme.size),
  );
  if (preview.isBinary) return undefined;
  return {
    path: readme.path,
    name: readme.name,
    content: preview.content,
    isTruncated: preview.isTruncated,
  } as RepositoryReadme;
}

async function readBlobResponse(
  gitDir: string,
  commitHash: string,
  repoId: number,
  refName: string,
  repoPath: string,
): Promise<GetBlobResponse> {
  const objectSpec = `${commitHash}:${repoPath}`;
  const objectType = await runGitText(gitDir, ['cat-file', '-t', objectSpec]);
  if (objectType.trim() !== 'blob') {
    throw new ConnectError('file not found', Code.NotFound);
  }

  const size = Number(await runGitText(gitDir, ['cat-file', '-s', objectSpec]));
  const preview = await readBlobPreview(gitDir, objectSpec, size);
  const hash = await runGitText(gitDir, ['rev-parse', objectSpec]);

  return {
    repoId: BigInt(repoId),
    refName,
    commitHash,
    path: repoPath,
    name: basename(repoPath),
    mode: '',
    hash: hash.trim(),
    size: BigInt(size),
    content: preview.content,
    isBinary: preview.isBinary,
    isTruncated: preview.isTruncated,
  } as GetBlobResponse;
}

async function readBlobPreview(
  gitDir: string,
  objectSpec: string,
  size: number,
): Promise<{
  content: string;
  isBinary: boolean;
  isTruncated: boolean;
}> {
  const { data, truncated } = await runGitBufferLimited(gitDir, [
    'cat-file',
    '-p',
    objectSpec,
  ]);
  const clipped =
    data.length > MAX_TEXT_BLOB_BYTES
      ? data.subarray(0, MAX_TEXT_BLOB_BYTES)
      : data;
  const content = decodeUtf8Text(clipped);

  return {
    content: content ?? '',
    isBinary: content === null,
    isTruncated: truncated || size > MAX_TEXT_BLOB_BYTES,
  };
}

function decodeUtf8Text(data: Buffer): string | null {
  if (data.includes(0)) return null;
  try {
    return new TextDecoder('utf-8', { fatal: true }).decode(data);
  } catch {
    return null;
  }
}

async function getBlame(
  gitDir: string,
  commitHash: string,
  repoPath: string,
): Promise<{ lines: Array<BlameLine>; isTruncated: boolean }> {
  let stdout: string;
  try {
    stdout = await runGitText(
      gitDir,
      ['blame', '--line-porcelain', commitHash, '--', repoPath],
      50 * 1024 * 1024,
    );
  } catch (error) {
    throw new ConnectError(
      error instanceof Error ? error.message : 'file blame is not available',
      Code.NotFound,
    );
  }

  const parsed = parseBlamePorcelain(stdout);
  return {
    lines: parsed.slice(0, MAX_BLAME_LINES),
    isTruncated: parsed.length > MAX_BLAME_LINES,
  };
}

function parseBlamePorcelain(output: string): Array<BlameLine> {
  const lines: Array<BlameLine> = [];
  const records = output.split('\n');
  let index = 0;

  while (index < records.length) {
    const header = /^([0-9a-f]{40}) \d+ (\d+)(?: \d+)?$/.exec(
      records[index] ?? '',
    );
    if (!header) {
      index++;
      continue;
    }

    const [, commitHash, lineNo] = header;
    const blameLine: BlameLine = {
      commitHash,
      lineNo: Number(lineNo),
      authorName: '',
      authorEmail: '',
      summary: '',
      content: '',
    } as BlameLine;

    index++;
    while (index < records.length) {
      const record = records[index] ?? '';
      index++;

      if (record.startsWith('\t')) {
        blameLine.content = record.slice(1);
        lines.push(blameLine);
        break;
      }

      const separator = record.indexOf(' ');
      const key = separator >= 0 ? record.slice(0, separator) : record;
      const value = separator >= 0 ? record.slice(separator + 1) : '';
      switch (key) {
        case 'author':
          blameLine.authorName = value;
          break;
        case 'author-mail':
          blameLine.authorEmail = value.replace(/^<|>$/g, '');
          break;
        case 'author-time':
          blameLine.authoredAt = blameTimestamp(value);
          break;
        case 'summary':
          blameLine.summary = value;
          break;
      }
    }
  }

  return lines;
}

function blameTimestamp(value: string) {
  const seconds = Number(value);
  if (!Number.isFinite(seconds)) return undefined;
  return toTimestamp(new Date(seconds * 1000).toISOString());
}

async function listCommits(
  gitDir: string,
  commitHash: string,
  limit: number,
  offset: number,
  pathFilter: string,
): Promise<Array<RepositoryCommit>> {
  const args = [
    'log',
    `--max-count=${limit}`,
    `--skip=${offset}`,
    '--format=%H%x1f%P%x1f%aI%x1f%cI%x1f%an%x1f%ae%x1f%cn%x1f%ce%x1f%B%x1e',
    commitHash,
  ];
  if (pathFilter) {
    args.push('--', pathFilter);
  }

  const stdout = await runGitText(gitDir, args, 20 * 1024 * 1024);

  return stdout
    .split('\x1e')
    .map((record) => record.replace(/^\n+/, ''))
    .filter(Boolean)
    .map(parseCommitRecord);
}

async function listCompareCommits(
  gitDir: string,
  baseCommitHash: string,
  headCommitHash: string,
  limit: number,
): Promise<Array<RepositoryCommit>> {
  const stdout = await runGitText(
    gitDir,
    [
      'log',
      `--max-count=${limit}`,
      '--format=%H%x1f%P%x1f%aI%x1f%cI%x1f%an%x1f%ae%x1f%cn%x1f%ce%x1f%B%x1e',
      `${baseCommitHash}..${headCommitHash}`,
    ],
    20 * 1024 * 1024,
  );

  return stdout
    .split('\x1e')
    .map((record) => record.replace(/^\n+/, ''))
    .filter(Boolean)
    .map(parseCommitRecord);
}

function parseCommitRecord(record: string): RepositoryCommit {
  const parts = record.split('\x1f');
  if (parts.length < 9) {
    throw new ConnectError('invalid git commit record', Code.Internal);
  }
  const [
    hash,
    parentHashes,
    authoredAt,
    committedAt,
    authorName,
    authorEmail,
    committerName,
    committerEmail,
    ...messageParts
  ] = parts;

  return {
    hash,
    parentHashes: parentHashes ? parentHashes.split(' ') : [],
    authoredAt: toTimestamp(authoredAt),
    committedAt: toTimestamp(committedAt),
    authorName,
    authorEmail,
    committerName,
    committerEmail,
    message: messageParts.join('\x1f'),
  } as RepositoryCommit;
}

async function getCommit(
  gitDir: string,
  commitHash: string,
): Promise<RepositoryCommit> {
  const stdout = await runGitText(
    gitDir,
    [
      'show',
      '--no-patch',
      '--format=%H%x1f%P%x1f%aI%x1f%cI%x1f%an%x1f%ae%x1f%cn%x1f%ce%x1f%B%x1e',
      commitHash,
    ],
    2 * 1024 * 1024,
  );
  const record = stdout.split('\x1e').find(Boolean);
  if (!record) {
    throw new ConnectError('commit object not found', Code.NotFound);
  }
  return parseCommitRecord(record.replace(/^\n+/, ''));
}

async function getCommitPatch(
  gitDir: string,
  commitHash: string,
): Promise<{ patch: string; isTruncated: boolean }> {
  const output = await runGitBuffer(
    gitDir,
    [
      'show',
      '--format=',
      '--patch',
      '--find-renames',
      '--find-copies',
      '--no-ext-diff',
      '--no-color',
      '--root',
      commitHash,
    ],
    20 * 1024 * 1024,
  );
  const isTruncated = output.length > MAX_COMMIT_PATCH_BYTES;
  const clipped = isTruncated
    ? output.subarray(0, MAX_COMMIT_PATCH_BYTES)
    : output;
  return { patch: clipped.toString('utf8'), isTruncated };
}

async function getRevisionDiffPatch(
  gitDir: string,
  baseCommitHash: string,
  headCommitHash: string,
): Promise<{ patch: string; isTruncated: boolean }> {
  const output = await runGitBuffer(
    gitDir,
    [
      'diff',
      '--patch',
      '--find-renames',
      '--find-copies',
      '--no-ext-diff',
      '--no-color',
      baseCommitHash,
      headCommitHash,
    ],
    20 * 1024 * 1024,
  );
  const isTruncated = output.length > MAX_COMMIT_PATCH_BYTES;
  const clipped = isTruncated
    ? output.subarray(0, MAX_COMMIT_PATCH_BYTES)
    : output;
  return { patch: clipped.toString('utf8'), isTruncated };
}

async function countRevisionRange(
  gitDir: string,
  baseCommitHash: string,
  headCommitHash: string,
): Promise<number> {
  const stdout = await runGitText(gitDir, [
    'rev-list',
    '--count',
    `${baseCommitHash}..${headCommitHash}`,
  ]);
  const count = Number(stdout.trim());
  return Number.isFinite(count) ? count : 0;
}

async function findMergeBase(
  gitDir: string,
  baseCommitHash: string,
  headCommitHash: string,
): Promise<string> {
  try {
    return (
      await runGitText(gitDir, ['merge-base', baseCommitHash, headCommitHash])
    ).trim();
  } catch (error) {
    if (isGitExitCode(error, 1)) return '';
    throw error;
  }
}

function parseCommitPatch(
  patch: string,
  patchTruncated: boolean,
): {
  files: Array<CommitFileDiff>;
  additions: number;
  deletions: number;
} {
  const files: Array<CommitFileDiff> = [];
  let current: CommitDiffDraft | undefined;
  let additions = 0;
  let deletions = 0;

  const flush = () => {
    if (!current) return;
    current.status ||= 'modified';
    current.newPath ||= current.oldPath;
    current.oldPath ||= current.newPath;
    additions += current.additions;
    deletions += current.deletions;
    files.push({
      oldPath: current.oldPath,
      newPath: current.newPath,
      status: current.status,
      additions: current.additions,
      deletions: current.deletions,
      patch: current.patch,
      isBinary: current.isBinary,
      isTruncated: current.isTruncated,
    } as CommitFileDiff);
  };

  for (const line of patch.match(/[^\n]*\n|[^\n]+/g) ?? []) {
    if (line.startsWith('diff --git ')) {
      flush();
      current = newCommitDiffDraft(line);
      continue;
    }
    if (!current) continue;
    current.patch += line;
    updateCommitDiffDraft(current, line);
  }

  if (patchTruncated && current) current.isTruncated = true;
  flush();

  return { files, additions, deletions };
}

type CommitDiffDraft = {
  oldPath: string;
  newPath: string;
  status: string;
  additions: number;
  deletions: number;
  patch: string;
  isBinary: boolean;
  isTruncated: boolean;
};

function newCommitDiffDraft(header: string): CommitDiffDraft {
  const parts = header.trim().split(/\s+/);
  return {
    oldPath: parts[2] ? trimGitDiffPath(parts[2], 'a/') : '',
    newPath: parts[3] ? trimGitDiffPath(parts[3], 'b/') : '',
    status: 'modified',
    additions: 0,
    deletions: 0,
    patch: header,
    isBinary: false,
    isTruncated: false,
  };
}

function updateCommitDiffDraft(draft: CommitDiffDraft, line: string): void {
  const trimmed = line.replace(/[\r\n]+$/, '');

  if (trimmed.startsWith('new file mode ')) {
    draft.status = 'added';
    return;
  }
  if (trimmed.startsWith('deleted file mode ')) {
    draft.status = 'deleted';
    return;
  }
  if (trimmed.startsWith('rename from ')) {
    draft.status = 'renamed';
    draft.oldPath = trimmed.slice('rename from '.length);
    return;
  }
  if (trimmed.startsWith('rename to ')) {
    draft.status = 'renamed';
    draft.newPath = trimmed.slice('rename to '.length);
    return;
  }
  if (trimmed.startsWith('copy from ')) {
    draft.status = 'copied';
    draft.oldPath = trimmed.slice('copy from '.length);
    return;
  }
  if (trimmed.startsWith('copy to ')) {
    draft.status = 'copied';
    draft.newPath = trimmed.slice('copy to '.length);
    return;
  }
  if (trimmed.startsWith('--- ')) {
    const value = parsePatchPath(trimmed, '--- ', 'a/');
    if (value && value !== '/dev/null') draft.oldPath = value;
    return;
  }
  if (trimmed.startsWith('+++ ')) {
    const value = parsePatchPath(trimmed, '+++ ', 'b/');
    if (value && value !== '/dev/null') draft.newPath = value;
    return;
  }
  if (
    trimmed.startsWith('Binary files ') ||
    trimmed.startsWith('GIT binary patch')
  ) {
    draft.isBinary = true;
    return;
  }
  if (line.startsWith('+') && !line.startsWith('+++')) {
    draft.additions++;
    return;
  }
  if (line.startsWith('-') && !line.startsWith('---')) {
    draft.deletions++;
  }
}

function parsePatchPath(line: string, prefix: string, marker: string): string {
  return trimGitDiffPath(line.slice(prefix.length).trim(), marker);
}

function trimGitDiffPath(value: string, marker: string): string {
  const trimmed = value.replace(/^"|"$/g, '');
  if (trimmed === '/dev/null') return trimmed;
  return trimmed.startsWith(marker) ? trimmed.slice(marker.length) : trimmed;
}

async function runGitText(
  gitDir: string,
  args: Array<string>,
  maxBuffer = 1024 * 1024,
): Promise<string> {
  const result = (await execFileAsync('git', ['--git-dir', gitDir, ...args], {
    encoding: 'utf8',
    maxBuffer,
  })) as { stdout: string };
  return result.stdout;
}

async function runGitBuffer(
  gitDir: string,
  args: Array<string>,
  maxBuffer = 20 * 1024 * 1024,
): Promise<Buffer> {
  const result = (await execFileAsync('git', ['--git-dir', gitDir, ...args], {
    encoding: 'buffer',
    maxBuffer,
  })) as { stdout: Buffer };
  return result.stdout;
}

function runGitBufferLimited(
  gitDir: string,
  args: Array<string>,
): Promise<{ data: Buffer; truncated: boolean }> {
  return new Promise((resolve, reject) => {
    const child = spawn('git', ['--git-dir', gitDir, ...args], {
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    const chunks: Array<Buffer> = [];
    const stderr: Array<Buffer> = [];
    let total = 0;
    let truncated = false;
    const limit = MAX_TEXT_BLOB_BYTES + 1;

    child.stdout.on('data', (chunk: Buffer) => {
      const remaining = limit - total;
      if (remaining > 0) {
        const nextChunk =
          chunk.length > remaining ? chunk.subarray(0, remaining) : chunk;
        chunks.push(nextChunk);
        total += nextChunk.length;
      }
      if (total >= limit && !truncated) {
        truncated = true;
        child.kill();
      }
    });
    child.stderr.on('data', (chunk: Buffer) => stderr.push(chunk));
    child.on('error', reject);
    child.on('close', (code) => {
      if (code === 0 || truncated) {
        resolve({ data: Buffer.concat(chunks), truncated });
        return;
      }
      reject(
        new Error(
          Buffer.concat(stderr).toString('utf8').trim() ||
            `git exited with code ${code}`,
        ),
      );
    });
  });
}

function repoToProto(repo: RepoRow): Repository {
  return {
    id: BigInt(repo.id),
    sourceId: repo.source_id,
    platform: repo.platform,
    name: repo.name,
    fullName: repo.full_name,
    owner: repo.owner,
    description: repo.description ?? '',
    htmlUrl: repo.html_url ?? '',
    cloneUrl: repo.clone_url ?? '',
    sshUrl: repo.ssh_url ?? '',
    defaultBranch: repo.default_branch ?? '',
    visibility: repo.visibility ?? '',
    isPrivate: repo.is_private === 1,
    isFork: repo.is_fork === 1,
    isArchived: repo.is_archived === 1,
    status: repo.status,
    lastSeenAt: toTimestamp(repo.last_seen_at),
    createdAt: toTimestamp(repo.created_at),
    updatedAt: toTimestamp(repo.updated_at),
    meta: parseJsonObject(repo.meta),
    archiveRepoSizeBytes:
      repo.archive_repo_size_bytes === null
        ? undefined
        : BigInt(repo.archive_repo_size_bytes),
  } as Repository;
}

function refToProto(row: RepoRefRow): RepoRef {
  return {
    id: BigInt(row.id),
    refName: row.ref_name,
    refKind: row.ref_kind,
    currentHash: row.current_hash,
    status: row.status,
    archiveRefName: row.archive_ref_name ?? '',
    firstSeenAt: toTimestamp(row.first_seen_at),
    lastSeenAt: toTimestamp(row.last_seen_at),
    deletedAt: toTimestamp(row.deleted_at),
    lastHashUpdatedAt: toTimestamp(row.last_hash_updated_at),
    currentCommit: commitInfo(row),
  } as RepoRef;
}

type RepoRefChangeRow = {
  id: number;
  ref_name: string;
  ref_kind: string;
  action: string;
  old_hash: string | null;
  new_hash: string | null;
  archive_ref_name: string | null;
  created_at: string;
  new_commit_authored_at: string | null;
  new_commit_committed_at: string | null;
  new_commit_author_name: string | null;
  new_commit_author_email: string | null;
  new_commit_message: string | null;
};

function changeToProto(row: RepoRefChangeRow): RepoRefChange {
  return {
    id: BigInt(row.id),
    refName: row.ref_name,
    refKind: row.ref_kind,
    action: row.action,
    oldHash: row.old_hash ?? '',
    newHash: row.new_hash ?? '',
    archiveRefName: row.archive_ref_name ?? '',
    createdAt: toTimestamp(row.created_at),
    newCommit: {
      authoredAt: toTimestamp(row.new_commit_authored_at),
      committedAt: toTimestamp(row.new_commit_committed_at),
      authorName: row.new_commit_author_name ?? '',
      authorEmail: row.new_commit_author_email ?? '',
      message: row.new_commit_message ?? '',
    } as CommitInfo,
  } as RepoRefChange;
}

function commitInfo(row: RepoRefRow): CommitInfo | undefined {
  if (
    !row.current_commit_authored_at &&
    !row.current_commit_committed_at &&
    !row.current_commit_author_name &&
    !row.current_commit_author_email &&
    !row.current_commit_message
  ) {
    return undefined;
  }
  return {
    authoredAt: toTimestamp(row.current_commit_authored_at),
    committedAt: toTimestamp(row.current_commit_committed_at),
    authorName: row.current_commit_author_name ?? '',
    authorEmail: row.current_commit_author_email ?? '',
    message: row.current_commit_message ?? '',
  } as CommitInfo;
}
