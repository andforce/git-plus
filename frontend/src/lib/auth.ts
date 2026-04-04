const AUTH_TOKEN_KEY = 'git-plus-auth-token';

let authRequiredCallback: (() => void) | null = null;

export function getToken(): string | null {
  return globalThis.localStorage?.getItem(AUTH_TOKEN_KEY) ?? null;
}

export function setToken(token: string) {
  globalThis.localStorage?.setItem(AUTH_TOKEN_KEY, token);
}

export function clearToken() {
  globalThis.localStorage?.removeItem(AUTH_TOKEN_KEY);
}

export function onAuthRequired(cb: () => void) {
  authRequiredCallback = cb;
}

export function triggerAuthRequired() {
  authRequiredCallback?.();
}
