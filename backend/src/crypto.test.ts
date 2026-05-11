import { describe, expect, it } from 'vitest';
import { decryptToken, encryptToken, isEncryptedToken } from './crypto';

describe('token encryption', () => {
  it('round trips encrypted tokens with the Go-compatible prefix', () => {
    const encrypted = encryptToken('ghp_example', 'stable-passphrase');

    expect(isEncryptedToken(encrypted)).toBe(true);
    expect(decryptToken(encrypted, 'stable-passphrase')).toBe('ghp_example');
  });

  it('rejects the wrong passphrase', () => {
    const encrypted = encryptToken('ghp_example', 'stable-passphrase');

    expect(() => decryptToken(encrypted, 'wrong-passphrase')).toThrow();
  });
});
