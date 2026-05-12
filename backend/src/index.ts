import { existsSync } from 'node:fs';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import process from 'node:process';
import { fastify } from 'fastify';
import fastifyStatic from '@fastify/static';
import { fastifyConnectPlugin } from '@connectrpc/connect-fastify';
import { encryptToken } from './crypto';
import { CronRuntime } from './cron-runtime';
import { openDatabase } from './database';
import { DownloadManager, handleDownloadRequest } from './download';
import { EventBus } from './event-bus';
import {
  createRoutes,
  enqueueFullSyncTask,
  handleRawBlobRequest,
} from './routes';
import {
  INSECURE_NO_AUTH_PASSWORD,
  PASSWORD_ENV,
  applyRuntimeTokenPassphrase,
  completeRuntimeSetup,
  loadRuntimeSecurity,
  publicSetupState,
  resolveDataDir,
  verifyRuntimeAuth,
} from './runtime-settings';
import { selectSystemDirectory } from './system-directory-picker';
import { TaskManager } from './task-manager';
import type { AppDatabase } from './types';
import type { RuntimeDeps } from './routes';
import type { FastifyInstance, FastifyReply, FastifyRequest } from 'fastify';

const DEFAULT_PORT = 8000;

async function main(): Promise<void> {
  const rawArgs = process.argv.slice(2);
  if (rawArgs[0] === 'config' && rawArgs[1] === 'encrypt-token') {
    await encryptTokenCommand();
    return;
  }

  const options = parseArgs(rawArgs);

  let dataDir = resolveDataDir(options.dataDir);
  let security = loadRuntimeSecurity(dataDir);
  applyRuntimeTokenPassphrase(security);

  const bus = new EventBus();
  const downloads = new DownloadManager();
  let db: AppDatabase;
  let tasks: TaskManager;
  let cron: CronRuntime;
  const syncDeps: Omit<RuntimeDeps, 'cron'> = {
    get dataDir() {
      return dataDir;
    },
    get db() {
      return db;
    },
    get bus() {
      return bus;
    },
    get tasks() {
      return tasks;
    },
    get downloads() {
      return downloads;
    },
  };
  const runtimeDeps: RuntimeDeps = {
    get dataDir() {
      return dataDir;
    },
    get db() {
      return db;
    },
    get bus() {
      return bus;
    },
    get tasks() {
      return tasks;
    },
    get downloads() {
      return downloads;
    },
    get cron() {
      return cron;
    },
  };
  ({ db, tasks, cron } = openRuntime(dataDir, bus, syncDeps));
  const server = fastify({ logger: true });
  warnStartupEnvironment(server, security);
  server.log.info({ dataDir }, 'runtime data directory');

  server.addHook('onRequest', async (request, reply) => {
    if (!request.url.startsWith('/api')) return;
    if (isSetupRoute(request.url)) return;
    if (security.setupState.requiresSetup) {
      reply
        .code(401)
        .type('text/plain; charset=utf-8')
        .send('setup required\n');
      return;
    }
    const token = requestAuthToken(request.headers.authorization);
    if (!verifyRuntimeAuth(security, token)) {
      reply.code(401).type('text/plain; charset=utf-8').send('unauthorized\n');
      return;
    }
  });

  server.get('/api/setup', () => publicSetupState(security.setupState));
  server.post('/api/setup/select-data-dir', async (_request, reply) => {
    if (!security.setupState.requiresSetup) {
      reply
        .code(409)
        .type('text/plain; charset=utf-8')
        .send('setup is already complete\n');
      return;
    }

    try {
      const selectedDataDir = await selectSystemDirectory();
      if (!selectedDataDir) {
        reply.code(204).send();
        return;
      }

      const normalizedDataDir = resolveDataDir(selectedDataDir);
      if (normalizedDataDir !== dataDir) {
        const previousRuntime = { db, tasks, cron };
        const nextRuntime = openRuntime(normalizedDataDir, bus, syncDeps);
        dataDir = normalizedDataDir;
        db = nextRuntime.db;
        tasks = nextRuntime.tasks;
        cron = nextRuntime.cron;
        closeRuntime(previousRuntime);
        server.log.info({ dataDir }, 'runtime data directory changed');
      }

      security = loadRuntimeSecurity(dataDir);
      applyRuntimeTokenPassphrase(security);
      reply.send(publicSetupState(security.setupState));
    } catch (error) {
      reply
        .code(400)
        .type('text/plain; charset=utf-8')
        .send(`${(error as Error).message}\n`);
    }
  });

  server.post('/api/setup', (request, reply) => {
    try {
      const body = parseSetupBody(request.body);
      const setupState = completeRuntimeSetup(dataDir, body);
      security = loadRuntimeSecurity(dataDir);
      applyRuntimeTokenPassphrase(security);
      reply.send(setupState);
    } catch (error) {
      reply
        .code(400)
        .type('text/plain; charset=utf-8')
        .send(`${(error as Error).message}\n`);
    }
  });

  await server.register(fastifyConnectPlugin, {
    prefix: '/api',
    routes: createRoutes(runtimeDeps),
  });

  server.get(
    '/api/repos/:repoId/downloads/:downloadId/archive',
    (request, reply) => handleDownloadRequest(downloads, request, reply),
  );
  server.get('/api/repos/:repoId/raw', (request, reply) =>
    handleRawBlobRequest(runtimeDeps, request, reply),
  );
  server.get('/ready', () => 'ok\n');
  server.get('/healthz', () => 'ok\n');

  if (process.env.FRONTEND_DEV_SERVER) {
    server.all('/*', (request, reply) =>
      proxyToFrontendDevServer(request, reply),
    );
  } else {
    await registerStaticFrontend(server);
  }

  const port = options.port ?? portFromEnv() ?? DEFAULT_PORT;
  const host = options.host ?? '0.0.0.0';
  await server.listen({ host, port });

  const close = async () => {
    closeRuntime({ db, tasks, cron });
    await server.close();
  };
  process.once('SIGINT', () => void close().then(() => process.exit(0)));
  process.once('SIGTERM', () => void close().then(() => process.exit(0)));
}

async function encryptTokenCommand(): Promise<void> {
  const passphrase = process.env.ENCRYPTION_PASSPHRASE ?? '';
  if (!passphrase) throw new Error('ENCRYPTION_PASSPHRASE is required');
  const input = (await readStdin()).trim();
  if (!input) throw new Error('token is required on stdin');
  console.log(encryptToken(input, passphrase));
}

async function readStdin(): Promise<string> {
  const chunks: Array<Buffer> = [];
  for await (const chunk of process.stdin) {
    chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk));
  }
  return Buffer.concat(chunks).toString('utf8');
}

function parseArgs(args: Array<string>): {
  dataDir?: string;
  port?: number;
  host?: string;
} {
  let dataDir: string | undefined;
  let port: number | undefined;
  let host: string | undefined;
  for (let index = 0; index < args.length; index++) {
    const arg = args[index];
    if (arg === '--data-dir') dataDir = args[++index] ?? '';
    else if (arg === '--port' || arg === '--listen') {
      const value = args[++index] ?? '';
      port = Number.parseInt(value.replace(/^.*:/, ''), 10);
      const maybeHost = value.includes(':') ? value.replace(/:\d+$/, '') : '';
      host = maybeHost || host;
    }
  }
  return { dataDir, port, host };
}

function openRuntime(
  dataDir: string,
  bus: EventBus,
  syncDeps: Omit<RuntimeDeps, 'cron'>,
): Pick<RuntimeDeps, 'db' | 'tasks' | 'cron'> {
  const db = openDatabase(dataDir);
  const tasks = new TaskManager(db, bus);
  const cron = new CronRuntime(dataDir, () => {
    enqueueFullSyncTask(syncDeps);
  });
  cron.reload();

  return { db, tasks, cron };
}

function closeRuntime(runtime: Pick<RuntimeDeps, 'db' | 'tasks' | 'cron'>) {
  runtime.cron.close();
  runtime.tasks.close();
  runtime.db.close();
}

function warnStartupEnvironment(
  server: FastifyInstance,
  security: ReturnType<typeof loadRuntimeSecurity>,
): void {
  if (process.env[PASSWORD_ENV] === INSECURE_NO_AUTH_PASSWORD) {
    server.log.warn(
      `warning: ${PASSWORD_ENV}="${INSECURE_NO_AUTH_PASSWORD}" disables API authentication`,
    );
  }
  if (security.setupState.requiresSetup) {
    server.log.info('first-run setup is required');
  }
}

function requestAuthToken(value: string | undefined): string {
  const auth = value?.trim() ?? '';
  if (!auth) return '';
  const lower = auth.toLowerCase();
  if (lower.startsWith('bearer ')) return auth.slice(7).trim();
  return auth;
}

function portFromEnv(): number | undefined {
  const raw = process.env.PORT;
  if (!raw) return undefined;
  const value = Number.parseInt(raw, 10);
  return Number.isFinite(value) ? value : undefined;
}

function isSetupRoute(url: string): boolean {
  const path = url.split('?')[0];
  return path === '/api/setup' || path === '/api/setup/select-data-dir';
}

function parseSetupBody(value: unknown): { password?: string } {
  if (value == null || typeof value !== 'object') return {};
  const candidate = value as { password?: unknown };
  return {
    password:
      typeof candidate.password === 'string' ? candidate.password : undefined,
  };
}

async function registerStaticFrontend(server: FastifyInstance): Promise<void> {
  const frontendDist = resolve(process.cwd(), 'frontend/dist');
  if (existsSync(frontendDist)) {
    await server.register(fastifyStatic, {
      root: frontendDist,
      prefix: '/',
      wildcard: false,
    });
  }
  server.setNotFoundHandler(async (request, reply) => {
    if (request.url.startsWith('/api')) {
      reply.code(404).send('not found');
      return;
    }
    const indexPath = resolve(frontendDist, 'index.html');
    if (!existsSync(indexPath)) {
      reply.code(404).send('frontend build was not found');
      return;
    }
    reply
      .type('text/html; charset=utf-8')
      .send(await readFile(indexPath, 'utf8'));
  });
}

async function proxyToFrontendDevServer(
  request: FastifyRequest,
  reply: FastifyReply,
): Promise<void> {
  const upstream = process.env.FRONTEND_DEV_SERVER;
  if (!upstream) {
    reply.code(404).send('frontend dev server is not configured');
    return;
  }
  const target = new URL(request.raw.url ?? '/', upstream);
  const method = request.method;
  const init: RequestInit & { duplex?: 'half' } = {
    method,
    headers: proxyRequestHeaders(request.headers),
  };

  if (method !== 'GET' && method !== 'HEAD') {
    init.body = request.raw as unknown as BodyInit;
    init.duplex = 'half';
  }

  const response = await fetch(target, init);
  reply.code(response.status);
  response.headers.forEach((value, key) => {
    if (shouldSkipProxyResponseHeader(key)) return;
    reply.header(key, value);
  });
  if (method === 'HEAD' || isBodylessResponseStatus(response.status)) {
    reply.send();
    return;
  }
  const body = Buffer.from(await response.arrayBuffer());
  reply.header('content-length', body.byteLength);
  reply.send(body);
}

function proxyRequestHeaders(headers: FastifyRequest['headers']): HeadersInit {
  const nextHeaders = new Headers();
  for (const [key, value] of Object.entries(headers)) {
    if (value === undefined || shouldSkipProxyRequestHeader(key)) continue;
    if (Array.isArray(value)) {
      for (const item of value) nextHeaders.append(key, item);
    } else {
      nextHeaders.set(key, String(value));
    }
  }
  return nextHeaders;
}

function shouldSkipProxyRequestHeader(header: string): boolean {
  const normalized = header.toLowerCase();
  return normalized === 'host' || hopByHopHeaders.has(normalized);
}

function shouldSkipProxyResponseHeader(header: string): boolean {
  const normalized = header.toLowerCase();
  return (
    normalized === 'content-length' ||
    normalized === 'content-encoding' ||
    hopByHopHeaders.has(normalized)
  );
}

function isBodylessResponseStatus(status: number): boolean {
  return status === 204 || status === 304;
}

const hopByHopHeaders = new Set([
  'connection',
  'keep-alive',
  'proxy-authenticate',
  'proxy-authorization',
  'te',
  'trailer',
  'transfer-encoding',
  'upgrade',
]);

void main();
