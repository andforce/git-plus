import { createConnectTransport } from '@connectrpc/connect-web';
import { clearToken, getToken, triggerAuthRequired } from '~lib/auth';

const apiBaseUrl = '/api';

function createAuthFetch(
  fetchFn: typeof globalThis.fetch = globalThis.fetch,
): typeof globalThis.fetch {
  return async (input, init) => {
    const token = getToken();
    const headers = new Headers(init?.headers);
    if (token) {
      headers.set('Authorization', `Bearer ${token}`);
    }
    const response = await fetchFn(input, { ...init, headers });
    if (response.status === 401) {
      clearToken();
      triggerAuthRequired();
    }
    return response;
  };
}

export function createApiTransport(
  baseUrl = apiBaseUrl,
  fetchFn = globalThis.fetch,
) {
  return createConnectTransport({
    baseUrl,
    fetch: createAuthFetch(fetchFn),
  });
}

export const apiTransport = createApiTransport();
