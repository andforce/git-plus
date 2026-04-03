import { createConnectTransport } from '@connectrpc/connect-web';

const apiBaseUrl = '/api';

export function createApiTransport(
  baseUrl = apiBaseUrl,
  fetchFn = globalThis.fetch,
) {
  return createConnectTransport({
    baseUrl,
    fetch: fetchFn,
  });
}

export const apiTransport = createApiTransport();
