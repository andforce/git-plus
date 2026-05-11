import { existsSync, mkdtempSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { configPath } from './config-store';
import {
  PASSWORD_ENV,
  completeRuntimeSetup,
  loadRuntimeSecurity,
  verifyRuntimeAuth,
} from './runtime-settings';

function tempDataDir(): string {
  return mkdtempSync(join(tmpdir(), 'git-plus-runtime-settings-'));
}

afterEach(() => {
  vi.unstubAllEnvs();
});

describe('runtime settings', () => {
  it('starts in setup mode when no local or environment secrets exist', () => {
    vi.stubEnv(PASSWORD_ENV, '');
    vi.stubEnv('ENCRYPTION_PASSPHRASE', '');

    const security = loadRuntimeSecurity(tempDataDir());

    expect(security.setupState.requiresSetup).toBe(true);
    expect(security.setupState.authConfigured).toBe(false);
    expect(security.setupState.encryptionConfigured).toBe(false);
  });

  it('completes setup with a local password and generated token passphrase', () => {
    vi.stubEnv(PASSWORD_ENV, '');
    vi.stubEnv('ENCRYPTION_PASSPHRASE', '');
    const dataDir = tempDataDir();

    const setup = completeRuntimeSetup(dataDir, {
      password: 'correct horse battery staple',
    });
    const security = loadRuntimeSecurity(dataDir);

    expect(setup.requiresSetup).toBe(false);
    expect(setup.authMode).toBe('local');
    expect(setup.configExists).toBe(true);
    expect(existsSync(configPath(dataDir))).toBe(true);
    expect(security.tokenPassphrase).not.toBe('');
    expect(verifyRuntimeAuth(security, 'correct horse battery staple')).toBe(
      true,
    );
    expect(verifyRuntimeAuth(security, 'wrong password')).toBe(false);
  });

  it('uses environment auth while generating only the missing passphrase', () => {
    vi.stubEnv(PASSWORD_ENV, 'dashboard-password');
    vi.stubEnv('ENCRYPTION_PASSPHRASE', '');
    const dataDir = tempDataDir();

    const setup = completeRuntimeSetup(dataDir, {});
    const security = loadRuntimeSecurity(dataDir);

    expect(setup.requiresSetup).toBe(false);
    expect(setup.authMode).toBe('environment');
    expect(verifyRuntimeAuth(security, 'dashboard-password')).toBe(true);
    expect(security.tokenPassphrase).not.toBe('');
  });

  it('lets setup replace an environment password with a local password', () => {
    vi.stubEnv(PASSWORD_ENV, 'environment-password');
    vi.stubEnv('ENCRYPTION_PASSPHRASE', '');
    const dataDir = tempDataDir();

    const setup = completeRuntimeSetup(dataDir, {
      password: 'local dashboard password',
    });
    const security = loadRuntimeSecurity(dataDir);

    expect(setup.requiresSetup).toBe(false);
    expect(setup.authMode).toBe('local');
    expect(verifyRuntimeAuth(security, 'local dashboard password')).toBe(true);
    expect(verifyRuntimeAuth(security, 'environment-password')).toBe(false);
  });

  it('rejects too-short local setup passwords', () => {
    vi.stubEnv(PASSWORD_ENV, '');
    vi.stubEnv('ENCRYPTION_PASSPHRASE', '');

    expect(() =>
      completeRuntimeSetup(tempDataDir(), { password: 'short' }),
    ).toThrow('password must be at least 8 characters');
  });
});
