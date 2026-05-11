const AUTH_TOKEN_KEY = 'git-plus-auth-token';

let authRequiredCallback: (() => void) | null = null;

export function getToken(): string | null {
  const storage = globalThis.localStorage;
  if (!storage || typeof storage.getItem !== 'function') return null;
  return storage.getItem(AUTH_TOKEN_KEY) ?? null;
}

export function setToken(token: string) {
  const storage = globalThis.localStorage;
  if (!storage || typeof storage.setItem !== 'function') return;
  storage.setItem(AUTH_TOKEN_KEY, token);
}

export function clearToken() {
  const storage = globalThis.localStorage;
  if (!storage || typeof storage.removeItem !== 'function') return;
  storage.removeItem(AUTH_TOKEN_KEY);
}

export function onAuthRequired(cb: () => void) {
  authRequiredCallback = cb;
}

export function triggerAuthRequired() {
  authRequiredCallback?.();
}
