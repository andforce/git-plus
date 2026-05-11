import { createReadStream, createWriteStream } from 'node:fs';
import { cp, mkdir, mkdtemp, readdir, rm, stat } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { basename, join, relative } from 'node:path';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import yazl from 'yazl';
import { Code, ConnectError } from '@connectrpc/connect';
import {
  DownloadStage,
  DownloadState,
} from '../../frontend/src/rpc/gitplus/repo/v1/repo_pb';
import type { StreamRepositoryDownloadResponse } from '../../frontend/src/rpc/gitplus/repo/v1/repo_pb';
import type { FastifyReply, FastifyRequest } from 'fastify';
import type { AppDatabase, RepoRefRow, RepoRow } from './types';

const execFileAsync = promisify(execFile);
const SESSION_TTL_MS = 15 * 60 * 1000;

type DownloadSession = {
  id: string;
  repoId: number;
  zipPath: string;
  filename: string;
  rootPath: string;
  expiresAt: number;
};

export class DownloadManager {
  #sessions = new Map<string, DownloadSession>();

  register(
    repoId: number,
    zipPath: string,
    filename: string,
    rootPath: string,
  ): DownloadSession {
    this.cleanupExpired();
    const session = {
      id: crypto.randomUUID(),
      repoId,
      zipPath,
      filename,
      rootPath,
      expiresAt: Date.now() + SESSION_TTL_MS,
    };
    this.#sessions.set(session.id, session);
    return session;
  }

  get(repoId: number, id: string): DownloadSession {
    this.cleanupExpired();
    const session = this.#sessions.get(id);
    if (!session || session.repoId !== repoId) {
      throw new ConnectError('download session not found', Code.NotFound);
    }
    return session;
  }

  cleanupExpired(): void {
    const now = Date.now();
    for (const [id, session] of this.#sessions) {
      if (session.expiresAt <= now) {
        this.#sessions.delete(id);
        void rm(session.rootPath, { recursive: true, force: true });
      }
    }
  }
}

export async function* streamRepositoryDownload(
  dataDir: string,
  db: AppDatabase,
  downloads: DownloadManager,
  repoId: bigint,
): AsyncGenerator<StreamRepositoryDownloadResponse> {
  const id = Number(repoId);
  if (!Number.isSafeInteger(id) || id <= 0) {
    throw new ConnectError('repo_id is required', Code.InvalidArgument);
  }
  const repo = db.prepare('SELECT * FROM repos WHERE id = ?').get(id) as
    | RepoRow
    | undefined;
  if (!repo) throw new ConnectError('repository not found', Code.NotFound);

  const refs = db
    .prepare(
      "SELECT * FROM repo_refs_current WHERE repo_id = ? AND status = 'active' ORDER BY ref_name",
    )
    .all(id) as Array<RepoRefRow>;
  if (refs.length === 0) {
    yield event(
      id,
      DownloadState.FAILED,
      DownloadStage.UNSPECIFIED,
      'No active branches or tags are available for download.',
      100,
    );
    return;
  }

  try {
    yield event(
      id,
      DownloadState.RUNNING,
      DownloadStage.COPY_BARE,
      'Copying bare repository to a temporary directory...',
      10,
    );
    const artifact = await prepareRepositoryDownload(
      dataDir,
      repo,
      refs,
      (stage, summary, percent) => {
        void stage;
        void summary;
        void percent;
        return Promise.resolve();
      },
    );
    const session = downloads.register(
      id,
      artifact.zipPath,
      artifact.filename,
      artifact.rootPath,
    );
    yield {
      repoId,
      state: DownloadState.READY,
      stage: DownloadStage.READY,
      summary: 'Download is ready.',
      progressPercent: 100,
      downloadId: session.id,
      downloadFilename: session.filename,
    } as StreamRepositoryDownloadResponse;
  } catch (error) {
    yield {
      repoId,
      state: DownloadState.FAILED,
      stage: DownloadStage.UNSPECIFIED,
      summary: 'Failed to prepare download.',
      progressPercent: 100,
      errorMessage: error instanceof Error ? error.message : String(error),
    } as StreamRepositoryDownloadResponse;
  }
}

export async function handleDownloadRequest(
  downloads: DownloadManager,
  request: FastifyRequest,
  reply: FastifyReply,
): Promise<void> {
  const params = request.params as { repoId?: string; downloadId?: string };
  const repoId = Number(params.repoId);
  if (!Number.isSafeInteger(repoId) || !params.downloadId) {
    reply.code(404).send('not found');
    return;
  }
  const session = downloads.get(repoId, params.downloadId);
  reply.header('Content-Type', 'application/zip');
  reply.header(
    'Content-Disposition',
    `attachment; filename="${session.filename.replaceAll('"', '')}"`,
  );
  await reply.send(createReadStream(session.zipPath));
}

async function prepareRepositoryDownload(
  dataDir: string,
  repo: RepoRow,
  refs: Array<RepoRefRow>,
  progress: (
    stage: DownloadStage,
    summary: string,
    percent: number,
  ) => Promise<void>,
) {
  const sourcePath = join(dataDir, 'repos', repo.source_id, repo.ref_id);
  const rootPath = await mkdtemp(join(tmpdir(), 'git-plus-repo-download-'));
  const barePath = join(rootPath, 'archive.git');
  const workPath = join(rootPath, sanitize(repo.name));
  const filename = `${sanitize(repo.name)}-${new Date().toISOString().slice(0, 10)}.zip`;
  const zipPath = join(rootPath, filename);

  await progress(
    DownloadStage.COPY_BARE,
    'Copying bare repository to a temporary directory...',
    10,
  );
  await cp(sourcePath, barePath, { recursive: true });

  await progress(
    DownloadStage.MATERIALIZE_REFS,
    'Restoring active branches and tags...',
    30,
  );
  const headBranch = await materializeRefs(barePath, repo, refs);

  await progress(
    DownloadStage.MATERIALIZE_REFS,
    'Creating a normal repository snapshot...',
    60,
  );
  await runGit(rootPath, ['clone', barePath, workPath]);
  await createLocalBranches(workPath, refs, headBranch);

  await progress(
    DownloadStage.PACKAGE_ZIP,
    'Packaging repository archive...',
    80,
  );
  await zipDirectory(workPath, zipPath, sanitize(repo.name));
  return { rootPath, zipPath, filename };
}

async function materializeRefs(
  barePath: string,
  repo: RepoRow,
  refs: Array<RepoRefRow>,
): Promise<string> {
  const branches: Array<string> = [];
  for (const ref of refs) {
    await runGit(process.cwd(), [
      '--git-dir',
      barePath,
      'update-ref',
      ref.ref_name,
      ref.current_hash,
    ]);
    if (ref.ref_kind === 'head')
      branches.push(ref.ref_name.slice('refs/heads/'.length));
  }
  branches.sort();
  const headBranch = chooseHeadBranch(repo.default_branch, branches);
  if (headBranch) {
    await runGit(process.cwd(), [
      '--git-dir',
      barePath,
      'symbolic-ref',
      'HEAD',
      `refs/heads/${headBranch}`,
    ]);
  }
  return headBranch;
}

async function createLocalBranches(
  workPath: string,
  refs: Array<RepoRefRow>,
  headBranch: string,
): Promise<void> {
  for (const ref of refs) {
    if (ref.ref_kind !== 'head') continue;
    const branchName = ref.ref_name.slice('refs/heads/'.length);
    if (branchName === headBranch) continue;
    await runGit(workPath, ['branch', '--force', branchName, ref.current_hash]);
  }
}

function chooseHeadBranch(
  defaultBranch: string | null,
  branches: Array<string>,
): string {
  if (branches.length === 0) return '';
  if (defaultBranch && branches.includes(defaultBranch)) return defaultBranch;
  return branches[0] ?? '';
}

async function zipDirectory(
  srcDir: string,
  zipPath: string,
  rootName: string,
): Promise<void> {
  await mkdir(join(zipPath, '..'), { recursive: true });
  const zip = new yazl.ZipFile();
  const out = createWriteStream(zipPath);
  const done = new Promise<void>((resolve, reject) => {
    out.on('close', resolve);
    out.on('error', reject);
    zip.outputStream.on('error', reject);
  });
  zip.outputStream.pipe(out);
  await addDirectoryToZip(zip, srcDir, rootName);
  zip.end();
  await done;
}

async function addDirectoryToZip(
  zip: yazl.ZipFile,
  dir: string,
  rootName: string,
): Promise<void> {
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    const rel = join(rootName, relative(dir, path));
    if (entry.isDirectory()) {
      await addDirectoryToZip(zip, path, join(rootName, entry.name));
    } else if (entry.isFile()) {
      const fileStat = await stat(path);
      zip.addFile(path, rel, { mtime: fileStat.mtime, mode: fileStat.mode });
    }
  }
}

function event(
  repoId: number,
  state: DownloadState,
  stage: DownloadStage,
  summary: string,
  progressPercent: number,
): StreamRepositoryDownloadResponse {
  return {
    repoId: BigInt(repoId),
    state,
    stage,
    summary,
    progressPercent,
  } as StreamRepositoryDownloadResponse;
}

async function runGit(cwd: string, args: Array<string>): Promise<void> {
  await execFileAsync('git', args, { cwd, maxBuffer: 20 * 1024 * 1024 });
}

function sanitize(value: string): string {
  return (
    basename(value)
      .replace(/[^a-zA-Z0-9._-]+/g, '-')
      .replace(/^-+|-+$/g, '') || 'repo'
  );
}
