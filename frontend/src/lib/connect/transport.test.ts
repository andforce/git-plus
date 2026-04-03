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
            issues: [
              {
                severity: 'SEVERITY_WARNING',
                code: 'config_not_found',
                message: 'config file does not exist',
              },
            ],
            summary: {
              error: 0,
              warning: 1,
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

    expect(response.issues).toHaveLength(1);
    expect(response.issues[0]?.code).toBe('config_not_found');
    expect(response.summary?.warning).toBe(1);
    expect(fetchSpy).toHaveBeenCalledTimes(1);
  });
});
