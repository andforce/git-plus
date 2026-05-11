import {
  existsSync,
  mkdirSync,
  readFileSync,
  renameSync,
  writeFileSync,
} from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { homedir, platform } from 'node:os';
import { randomBytes, scryptSync, timingSafeEqual } from 'node:crypto';
import YAML from 'yaml';
import {
  TOKEN_PASSPHRASE_ENV,
  configPath,
  defaultConfig,
  saveConfig,
} from './config-store';

export const APP_SETTINGS_FILENAME = 'app.yaml';
export const DATA_DIR_ENV = 'GIT_PLUS_DATA_DIR';
export const PASSWORD_ENV = 'PASSWORD';
export const INSECURE_NO_AUTH_PASSWORD = 'insecure-noauth';

const PASSWORD_HASH_PREFIX = '$scrypt$1$';
const PASSWORD_SALT_BYTES = 16;
const PASSWORD_KEY_BYTES = 32;
const MIN_PASSWORD_LENGTH = 8;
let runtimeManagedPassphrase = false;

type RuntimeSettings = {
  auth?: {
    password_hash?: string;
  };
  token_passphrase?: string;
  setup_completed_at?: string;
};

type RuntimeAuth =
  | { mode: 'disabled' }
  | { mode: 'environment'; password: string }
  | { mode: 'local'; passwordHash: string }
  | { mode: 'unset' };

export type SetupState = {
  requiresSetup: boolean;
  authConfigured: boolean;
  encryptionConfigured: boolean;
  configExists: boolean;
  authMode: 'disabled' | 'environment' | 'local' | 'unset';
  dataDir: string;
};

export type RuntimeSecurity = {
  auth: RuntimeAuth;
  tokenPassphrase: string;
  setupState: SetupState;
};

export function defaultDataDir(): string {
  const envDir = process.env[DATA_DIR_ENV]?.trim();
  if (envDir) return resolve(envDir);

  const home = homedir();
  if (platform() === 'darwin') {
    return join(home, 'Library', 'Application Support', 'Git Plus');
  }
  if (platform() === 'win32') {
    return join(
      process.env.APPDATA || join(home, 'AppData', 'Roaming'),
      'Git Plus',
    );
  }

  return join(
    process.env.XDG_DATA_HOME || join(home, '.local', 'share'),
    'git-plus',
  );
}

export function resolveDataDir(input?: string): string {
  const trimmed = input?.trim() ?? '';
  return resolve(trimmed || defaultDataDir());
}

export function loadRuntimeSecurity(dataDir: string): RuntimeSecurity {
  const settings = loadRuntimeSettings(dataDir);
  const auth = resolveAuth(settings);
  const tokenPassphrase =
    process.env[TOKEN_PASSPHRASE_ENV]?.trim() ||
    settings.token_passphrase?.trim() ||
    '';
  const authMode = auth.mode;
  const authConfigured = authMode !== 'unset';
  const encryptionConfigured = tokenPassphrase.length > 0;

  return {
    auth,
    tokenPassphrase,
    setupState: {
      requiresSetup: !authConfigured || !encryptionConfigured,
      authConfigured,
      encryptionConfigured,
      configExists: existsSync(configPath(dataDir)),
      authMode,
      dataDir,
    },
  };
}

export function applyRuntimeTokenPassphrase(security: RuntimeSecurity): void {
  if (process.env[TOKEN_PASSPHRASE_ENV] && !runtimeManagedPassphrase) {
    return;
  }
  if (security.tokenPassphrase) {
    process.env[TOKEN_PASSPHRASE_ENV] = security.tokenPassphrase;
    runtimeManagedPassphrase = true;
    return;
  }
  if (runtimeManagedPassphrase) {
    delete process.env[TOKEN_PASSPHRASE_ENV];
    runtimeManagedPassphrase = false;
  }
}

export function completeRuntimeSetup(
  dataDir: string,
  input: { password?: string },
): SetupState {
  const settings = loadRuntimeSettings(dataDir);
  const security = loadRuntimeSecurity(dataDir);
  const password = input.password?.trim() ?? '';

  if (password) {
    if (password.length < MIN_PASSWORD_LENGTH) {
      throw new Error(
        `password must be at least ${MIN_PASSWORD_LENGTH} characters`,
      );
    }
    settings.auth = { password_hash: hashPassword(password) };
  } else if (security.auth.mode === 'unset') {
    throw new Error(
      `password must be at least ${MIN_PASSWORD_LENGTH} characters`,
    );
  }

  if (!security.tokenPassphrase) {
    settings.token_passphrase = randomBytes(32).toString('base64url');
  }

  settings.setup_completed_at = new Date().toISOString();
  saveRuntimeSettings(dataDir, settings);

  if (!existsSync(configPath(dataDir))) {
    saveConfig(dataDir, defaultConfig());
  }

  return loadRuntimeSecurity(dataDir).setupState;
}

export function verifyRuntimeAuth(
  security: RuntimeSecurity,
  token: string,
): boolean {
  const candidate = token.trim();
  if (security.auth.mode === 'disabled') return true;
  if (!candidate) return false;

  if (security.auth.mode === 'environment') {
    return constantTimeEqual(candidate, security.auth.password);
  }
  if (security.auth.mode === 'local') {
    return verifyPassword(candidate, security.auth.passwordHash);
  }

  return false;
}

export function publicSetupState(state: SetupState): SetupState {
  if (state.requiresSetup) return state;
  return { ...state, dataDir: '' };
}

function settingsPath(dataDir: string): string {
  return join(dataDir, APP_SETTINGS_FILENAME);
}

function loadRuntimeSettings(dataDir: string): RuntimeSettings {
  try {
    const content = readFileSync(settingsPath(dataDir), 'utf8');
    const parsed = (YAML.parse(content) ?? {}) as Partial<RuntimeSettings>;
    return normalizeRuntimeSettings(parsed);
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === 'ENOENT') {
      return {};
    }
    throw error;
  }
}

function saveRuntimeSettings(dataDir: string, settings: RuntimeSettings): void {
  const path = settingsPath(dataDir);
  mkdirSync(dirname(path), { recursive: true });
  const tempPath = join(
    dirname(path),
    `.app-${process.pid}-${Date.now()}.yaml`,
  );
  writeFileSync(tempPath, YAML.stringify(normalizeRuntimeSettings(settings)), {
    mode: 0o600,
  });
  renameSync(tempPath, path);
}

function normalizeRuntimeSettings(
  input: Partial<RuntimeSettings>,
): RuntimeSettings {
  const passwordHash = input.auth?.password_hash?.trim() ?? '';
  return {
    auth: passwordHash ? { password_hash: passwordHash } : undefined,
    token_passphrase: input.token_passphrase?.trim() || undefined,
    setup_completed_at: input.setup_completed_at?.trim() || undefined,
  };
}

function resolveAuth(settings: RuntimeSettings): RuntimeAuth {
  const envPassword = process.env[PASSWORD_ENV]?.trim() ?? '';
  if (envPassword === INSECURE_NO_AUTH_PASSWORD) return { mode: 'disabled' };

  const passwordHash = settings.auth?.password_hash?.trim() ?? '';
  if (passwordHash) return { mode: 'local', passwordHash };

  if (envPassword) return { mode: 'environment', password: envPassword };

  return { mode: 'unset' };
}

function hashPassword(password: string): string {
  const salt = randomBytes(PASSWORD_SALT_BYTES);
  const key = derivePasswordKey(password, salt);
  return `${PASSWORD_HASH_PREFIX}${salt.toString('base64url')}$${key.toString('base64url')}`;
}

function verifyPassword(password: string, passwordHash: string): boolean {
  if (!passwordHash.startsWith(PASSWORD_HASH_PREFIX)) return false;
  const [saltEncoded = '', hashEncoded = ''] = passwordHash
    .slice(PASSWORD_HASH_PREFIX.length)
    .split('$');
  if (!saltEncoded || !hashEncoded) return false;

  const salt = Buffer.from(saltEncoded, 'base64url');
  const expected = Buffer.from(hashEncoded, 'base64url');
  const actual = derivePasswordKey(password, salt);
  if (actual.length !== expected.length) return false;

  return timingSafeEqual(actual, expected);
}

function derivePasswordKey(password: string, salt: Buffer): Buffer {
  return scryptSync(password, salt, PASSWORD_KEY_BYTES, {
    N: 32768,
    r: 8,
    p: 1,
    maxmem: 64 * 1024 * 1024,
  });
}

function constantTimeEqual(left: string, right: string): boolean {
  const leftBuffer = Buffer.from(left);
  const rightBuffer = Buffer.from(right);
  if (leftBuffer.length !== rightBuffer.length) return false;
  return timingSafeEqual(leftBuffer, rightBuffer);
}
