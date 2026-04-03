import { createClient } from '@connectrpc/connect';
import { describe, expect, it, vi } from 'vitest';
import { createApiTransport } from './transport';
import { ConfigService } from '~rpc/gitplus/config/v1/config_pb';

describe('createApiTransport', () => {
  it('sends ConfigService requests to the /api Connect base path', async () => {
    const fetchSpy = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      expect(String(input)).toBe(
        'http://example.test/api/gitplus.config.v1.ConfigService/CheckConfig',
      );
      expect(init?.method).toBe('POST');

      return Promise.resolve(
        new Response(
          JSON.stringify({
            path: '/tmp/config.yaml',
            exists: false,
            issues: [],
            summary: {
              error: 0,
              warning: 0,
              info: 0,
            },
          }),
          {
            status: 200,
            headers: {
              'Content-Type': 'application/json',
            },
          },
        ),
      );
    });

    const client = createClient(
      ConfigService,
      createApiTransport('http://example.test/api', fetchSpy as typeof fetch),
    );

    const response = await client.checkConfig({});

    expect(response.exists).toBe(false);
    expect(response.path).toBe('/tmp/config.yaml');
    expect(fetchSpy).toHaveBeenCalledTimes(1);
  });
});
