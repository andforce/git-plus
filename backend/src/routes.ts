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
  CommitInfo,
  RepoRef,
  RepoRefChange,
  Repository,
} from '../../frontend/src/rpc/gitplus/repo/v1/repo_pb';
import type { AppDatabase, RepoRefRow, RepoRow } from './types';
import type { JsonObject } from '@bufbuild/protobuf';
import type { ConnectRouter } from '@connectrpc/connect';

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
