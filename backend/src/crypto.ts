import {
  createCipheriv,
  createDecipheriv,
  randomBytes,
  scryptSync,
  timingSafeEqual,
} from 'node:crypto';

export const ENCRYPTED_TOKEN_PREFIX = '$encrypted$1$';
const SALT_BYTES = 16;
const KEY_BYTES = 32;
const NONCE_BYTES = 12;
const TAG_BYTES = 16;

function deriveKey(passphrase: string, salt: Buffer): Buffer {
  if (!passphrase) {
    throw new Error('token passphrase is required');
  }
  return scryptSync(passphrase, salt, KEY_BYTES, {
    N: 32768,
    r: 8,
    p: 1,
    maxmem: 64 * 1024 * 1024,
  });
}

export function encryptToken(plaintext: string, passphrase: string): string {
  const token = plaintext.trim();
  if (!token) {
    throw new Error('token is required');
  }
  const salt = randomBytes(SALT_BYTES);
  const nonce = randomBytes(NONCE_BYTES);
  const key = deriveKey(passphrase, salt);
  const cipher = createCipheriv('aes-256-gcm', key, nonce);
  const ciphertext = Buffer.concat([
    cipher.update(token, 'utf8'),
    cipher.final(),
    cipher.getAuthTag(),
  ]);
  return (
    ENCRYPTED_TOKEN_PREFIX +
    Buffer.concat([salt, nonce, ciphertext]).toString('base64url')
  );
}

export function decryptToken(ciphertext: string, passphrase: string): string {
  if (!ciphertext.startsWith(ENCRYPTED_TOKEN_PREFIX)) {
    throw new Error('token must use encrypted format');
  }
  const encoded = ciphertext.slice(ENCRYPTED_TOKEN_PREFIX.length);
  const payload = Buffer.from(encoded, 'base64url');
  if (payload.length < SALT_BYTES + NONCE_BYTES + TAG_BYTES) {
    throw new Error('invalid encrypted token');
  }
  const salt = payload.subarray(0, SALT_BYTES);
  const nonce = payload.subarray(SALT_BYTES, SALT_BYTES + NONCE_BYTES);
  const encrypted = payload.subarray(SALT_BYTES + NONCE_BYTES);
  const tag = encrypted.subarray(encrypted.length - TAG_BYTES);
  const body = encrypted.subarray(0, encrypted.length - TAG_BYTES);
  const decipher = createDecipheriv(
    'aes-256-gcm',
    deriveKey(passphrase, salt),
    nonce,
  );
  decipher.setAuthTag(tag);
  return Buffer.concat([decipher.update(body), decipher.final()]).toString(
    'utf8',
  );
}

export function isEncryptedToken(value: string): boolean {
  return value.startsWith(ENCRYPTED_TOKEN_PREFIX);
}

export function constantTimeEqual(left: string, right: string): boolean {
  const leftBuffer = Buffer.from(left);
  const rightBuffer = Buffer.from(right);
  if (leftBuffer.length !== rightBuffer.length) return false;
  return timingSafeEqual(leftBuffer, rightBuffer);
}
