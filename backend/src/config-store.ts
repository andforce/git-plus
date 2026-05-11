import { mkdirSync, readFileSync, renameSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { randomBytes } from 'node:crypto';
import YAML from 'yaml';
import { Code, ConnectError } from '@connectrpc/connect';
import {
  Platform,
  ValidationIssue_Severity,
} from '../../frontend/src/rpc/gitplus/config/v1/config_pb';
import {
  ENCRYPTED_TOKEN_PREFIX,
  encryptToken,
  isEncryptedToken,
} from './crypto';
import { compactString, nonEmptyArray } from './util';
import type {
  ConfigSnapshot,
  Source,
  ValidationIssue,
} from '../../frontend/src/rpc/gitplus/config/v1/config_pb';
import type { AppConfig, SourceConfig } from './types';

export const CONFIG_FILENAME = 'config.yaml';
export const TOKEN_PASSPHRASE_ENV = 'ENCRYPTION_PASSPHRASE';
export const DEFAULT_CONCURRENCY = 5;
export const DEFAULT_MAX_RETRY_TIMES = 2;

export function configPath(dataDir: string): string {
  return join(dataDir, CONFIG_FILENAME);
}

export function defaultConfig(): AppConfig {
  return {
    sources: [],
    concurrency: DEFAULT_CONCURRENCY,
    max_retry_times: DEFAULT_MAX_RETRY_TIMES,
    cron: '',
  };
}

export function effectiveSourceName(source: SourceConfig): string {
  return compactString(source.name) || source.id;
}

export function loadConfigOrDefault(dataDir: string): {
  config: AppConfig;
  exists: boolean;
} {
  const path = configPath(dataDir);
  try {
    const content = readFileSync(path, 'utf8');
    const parsed = (YAML.parse(content) ?? {}) as Partial<AppConfig>;
    return { config: normalizeConfig(parsed), exists: true };
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === 'ENOENT') {
      return { config: defaultConfig(), exists: false };
    }
    throw error;
  }
}

export function saveConfig(dataDir: string, config: AppConfig): void {
  const path = configPath(dataDir);
  mkdirSync(dirname(path), { recursive: true });
  const normalized = normalizeConfig(config);
  const tempPath = join(
    dirname(path),
    `.config-${process.pid}-${Date.now()}.yaml`,
  );
  writeFileSync(tempPath, YAML.stringify(normalized), { mode: 0o600 });
  renameSync(tempPath, path);
}

export function toConfigSnapshot(config: AppConfig): ConfigSnapshot {
  return {
    concurrency: config.concurrency,
    maxRetryTimes: config.max_retry_times,
    cron: config.cron ?? '',
    sources: [...config.sources]
      .sort(
        (a, b) =>
          effectiveSourceName(a).localeCompare(effectiveSourceName(b)) ||
          a.id.localeCompare(b.id),
      )
      .map(toProtoSource),
  } as ConfigSnapshot;
}

export function toProtoSource(source: SourceConfig): Source {
  return {
    id: source.id,
    platform: Platform.GITHUB,
    username: source.username,
    token: isEncryptedToken(source.token) ? source.token : '',
    onlyIncludeRepos: [...source.only_include_repos],
    excludeRepos: [...source.exclude_repos],
    includeDefaults: source.include_defaults,
    includeStarred: source.include_starred,
    includeWatching: source.include_watching,
    name: effectiveSourceName(source),
  } as Source;
}

export function createSource(input: {
  name?: string;
  platform: Platform;
  username: string;
  tokenPlaintext: string;
  onlyIncludeRepos?: Array<string>;
  excludeRepos?: Array<string>;
  includeDefaults?: boolean;
  includeStarred?: boolean;
  includeWatching?: boolean;
}): SourceConfig {
  if (input.platform !== Platform.GITHUB) {
    throw new ConnectError('unsupported platform', Code.InvalidArgument);
  }
  const username = compactString(input.username);
  if (!username) {
    throw new ConnectError('source.username is required', Code.InvalidArgument);
  }
  const passphrase = process.env[TOKEN_PASSPHRASE_ENV] ?? '';
  const token = encryptToken(input.tokenPlaintext, passphrase);
  return {
    id: generateSourceId(),
    name: compactString(input.name),
    platform: 'github',
    username,
    token,
    only_include_repos: uniqueStrings(nonEmptyArray(input.onlyIncludeRepos)),
    exclude_repos: uniqueStrings(nonEmptyArray(input.excludeRepos)),
    include_defaults: input.includeDefaults ?? true,
    include_starred: input.includeStarred ?? false,
    include_watching: input.includeWatching ?? false,
  };
}

export type SourcePatch = {
  platform?: Platform;
  username?: string;
  name?: string;
  onlyIncludeRepos?: Array<string>;
  excludeRepos?: Array<string>;
  includeDefaults?: boolean;
  includeStarred?: boolean;
  includeWatching?: boolean;
};

export function updateSource(
  source: SourceConfig,
  patch: SourcePatch,
): SourceConfig {
  const next = { ...source };
  if (patch.platform !== undefined && patch.platform !== Platform.GITHUB) {
    throw new ConnectError('unsupported platform', Code.InvalidArgument);
  }
  if (patch.username !== undefined) {
    const username = compactString(patch.username);
    if (!username)
      throw new ConnectError(
        'patch.username is required',
        Code.InvalidArgument,
      );
    next.username = username;
  }
  if (patch.name !== undefined) next.name = compactString(patch.name);
  if (patch.onlyIncludeRepos !== undefined) {
    next.only_include_repos = uniqueStrings(
      nonEmptyArray(patch.onlyIncludeRepos),
    );
  }
  if (patch.excludeRepos !== undefined) {
    next.exclude_repos = uniqueStrings(nonEmptyArray(patch.excludeRepos));
  }
  if (patch.includeDefaults !== undefined)
    next.include_defaults = patch.includeDefaults;
  if (patch.includeStarred !== undefined)
    next.include_starred = patch.includeStarred;
  if (patch.includeWatching !== undefined)
    next.include_watching = patch.includeWatching;
  return next;
}

export function checkConfig(
  dataDir: string,
  sourceId?: string,
): { exists: boolean; issues: Array<ValidationIssue> } {
  const issues: Array<ValidationIssue> = [];
  let loaded: { config: AppConfig; exists: boolean };
  try {
    loaded = loadConfigOrDefault(dataDir);
  } catch (error) {
    return {
      exists: true,
      issues: [
        issue(
          'ERROR',
          'config_invalid',
          `config file could not be read: ${(error as Error).message}`,
        ),
      ],
    };
  }
  if (!loaded.exists) {
    issues.push(
      issue('ERROR', 'config_not_found', 'config file does not exist'),
    );
    return { exists: false, issues };
  }
  const sources = sourceId
    ? loaded.config.sources.filter((source) => source.id === sourceId)
    : loaded.config.sources;
  if (sourceId && sources.length === 0) {
    issues.push(
      issue(
        'ERROR',
        'source_not_found',
        `source ${sourceId} was not found`,
        sourceId,
      ),
    );
  }
  for (const source of sources) {
    if (source.platform !== 'github') {
      issues.push(
        issue(
          'ERROR',
          'unsupported_platform',
          `unsupported platform ${source.platform}`,
          source.id,
        ),
      );
    }
    if (!source.username.trim()) {
      issues.push(
        issue('ERROR', 'username_required', 'username is required', source.id),
      );
    }
    if (!isEncryptedToken(source.token)) {
      issues.push(
        issue(
          'ERROR',
          'token_format',
          `token must use ${ENCRYPTED_TOKEN_PREFIX}... format`,
          source.id,
        ),
      );
    }
  }
  return { exists: loaded.exists, issues };
}

function normalizeConfig(input: Partial<AppConfig>): AppConfig {
  return {
    sources: Array.isArray(input.sources)
      ? input.sources.map((source) =>
          normalizeSource(source as Partial<SourceConfig>),
        )
      : [],
    concurrency: positiveInt(input.concurrency, DEFAULT_CONCURRENCY),
    max_retry_times: nonNegativeInt(
      input.max_retry_times,
      DEFAULT_MAX_RETRY_TIMES,
    ),
    cron: compactString(input.cron),
  };
}

function normalizeSource(input: Partial<SourceConfig>): SourceConfig {
  return {
    id: compactString(input.id),
    name: compactString(input.name),
    platform: 'github',
    username: compactString(input.username),
    token: compactString(input.token),
    only_include_repos: uniqueStrings(nonEmptyArray(input.only_include_repos)),
    exclude_repos: uniqueStrings(nonEmptyArray(input.exclude_repos)),
    include_defaults: input.include_defaults ?? true,
    include_starred: input.include_starred ?? false,
    include_watching: input.include_watching ?? false,
  };
}

function positiveInt(value: unknown, fallback: number): number {
  return typeof value === 'number' && Number.isInteger(value) && value > 0
    ? value
    : fallback;
}

function nonNegativeInt(value: unknown, fallback: number): number {
  return typeof value === 'number' && Number.isInteger(value) && value >= 0
    ? value
    : fallback;
}

function uniqueStrings(values: Array<string>): Array<string> {
  const seen = new Set<string>();
  for (const value of values) {
    if (seen.has(value)) {
      throw new ConnectError(`duplicate value ${value}`, Code.InvalidArgument);
    }
    seen.add(value);
  }
  return [...seen];
}

function generateSourceId(): string {
  return `src_${randomBytes(12).toString('hex')}`;
}

function issue(
  severity: 'ERROR' | 'WARNING' | 'INFO',
  code: string,
  message: string,
  sourceId = '',
): ValidationIssue {
  const severityValue =
    severity === 'ERROR'
      ? ValidationIssue_Severity.ERROR
      : severity === 'WARNING'
        ? ValidationIssue_Severity.WARNING
        : ValidationIssue_Severity.INFO;
  return {
    severity: severityValue,
    code,
    message,
    path: '',
    sourceId,
    line: 0,
  } as ValidationIssue;
}
