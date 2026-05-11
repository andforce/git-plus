import { existsSync } from 'node:fs';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { Readable } from 'node:stream';
import process from 'node:process';
import { fastify } from 'fastify';
import fastifyStatic from '@fastify/static';
import { fastifyConnectPlugin } from '@connectrpc/connect-fastify';
import { constantTimeEqual, encryptToken } from './crypto';
import { CronRuntime } from './cron-runtime';
import { openDatabase } from './database';
import { DownloadManager, handleDownloadRequest } from './download';
import { EventBus } from './event-bus';
import { createRoutes, enqueueFullSyncTask } from './routes';
import { TaskManager } from './task-manager';
import type { FastifyInstance, FastifyReply, FastifyRequest } from 'fastify';

const DEFAULT_PORT = 8080;
const INSECURE_NO_AUTH_PASSWORD = 'insecure-noauth';

async function main(): Promise<void> {
  const rawArgs = process.argv.slice(2);
  if (rawArgs[0] === 'config' && rawArgs[1] === 'encrypt-token') {
    await encryptTokenCommand();
    return;
  }

  const options = parseArgs(rawArgs);
  validateStartupEnvironment();

  const dataDir = resolve(options.dataDir);
  const db = openDatabase(dataDir);
  const bus = new EventBus();
  const tasks = new TaskManager(db, bus);
  const downloads = new DownloadManager();
  const baseDeps = { dataDir, db, bus, tasks, downloads };
  const cron = new CronRuntime(dataDir, () => {
    enqueueFullSyncTask(baseDeps);
  });
  cron.reload();
  const server = fastify({ logger: true });

  server.addHook('onRequest', async (request, reply) => {
    if (!request.url.startsWith('/api')) return;
    const password = process.env.PASSWORD ?? '';
    if (password === INSECURE_NO_AUTH_PASSWORD) return;
    const token = requestAuthToken(request.headers.authorization);
    if (!token || !constantTimeEqual(token, password)) {
      reply.code(401).type('text/plain; charset=utf-8').send('unauthorized\n');
    }
  });

  await server.register(fastifyConnectPlugin, {
    prefix: '/api',
    routes: createRoutes({ ...baseDeps, cron }),
  });

  server.get(
    '/api/repos/:repoId/downloads/:downloadId/archive',
    (request, reply) => handleDownloadRequest(downloads, request, reply),
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
    cron.close();
    tasks.close();
    db.close();
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
  dataDir: string;
  port?: number;
  host?: string;
} {
  let dataDir = '';
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
  if (!dataDir.trim()) throw new Error('--data-dir is required');
  return { dataDir, port, host };
}

function validateStartupEnvironment(): void {
  if (!process.env.ENCRYPTION_PASSPHRASE) {
    throw new Error('ENCRYPTION_PASSPHRASE is required');
  }
  if (!process.env.PASSWORD) {
    throw new Error('PASSWORD is required');
  }
  if (process.env.PASSWORD === INSECURE_NO_AUTH_PASSWORD) {
    console.warn(
      'warning: PASSWORD="insecure-noauth" disables API authentication',
    );
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
  const response = await fetch(target, {
    method,
    headers: request.headers as HeadersInit,
    body: method === 'GET' || method === 'HEAD' ? undefined : request.raw,
    duplex: 'half',
  } as RequestInit & { duplex: 'half' });
  reply.code(response.status);
  response.headers.forEach((value, key) => reply.header(key, value));
  if (!response.body) {
    reply.send();
    return;
  }
  reply.send(
    Readable.fromWeb(
      response.body as unknown as Parameters<typeof Readable.fromWeb>[0],
    ),
  );
}

void main();
